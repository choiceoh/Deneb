#!/usr/bin/env bash
# deploy.sh — Pull latest main and restart production gateway.
# Usage: scripts/deploy/deploy.sh [--build-only]
set -euo pipefail

PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
PROD_PORT="${DENEB_GATEWAY_PORT:-18789}"
GATEWAY_SERVICE="${DENEB_GATEWAY_SERVICE:-deneb-gateway.service}"
RESTART_MODE="${DENEB_DEPLOY_RESTART_MODE:-auto}" # auto | systemd | nohup
# A gateway restart can drain an already-running chat for up to six minutes,
# then spend bounded time closing the remaining subsystems. Keep the deploy
# waiter beyond the gateway's 8-minute force-exit watchdog so a healthy drain is
# never mistaken for a refused SIGUSR1 and escalated into a reply-killing restart.
RESTART_WAIT_SEC="${DENEB_DEPLOY_RESTART_WAIT_SEC:-510}"
# Remote deploy: when set, build locally (this host has Go + the git repo) and
# ship the binary to a gateway host that lacks a toolchain — instead of an
# in-place restart. This was the 2026-06-20~07-06 split (srv1 built, srv4 ran;
# srv1 was strict-overcommit so building beside prod was risky). Since the
# 2026-07-06 srv4 unification the gateway host builds in place (heuristic
# overcommit + user-space Go at ~/go-sdk/go), so this stays unset; the path is
# kept for any future host split. Empty = in-place restart (current default).
DEPLOY_REMOTE="${DENEB_DEPLOY_REMOTE:-}"            # e.g. "srv4" (ssh host)
DEPLOY_REMOTE_DIR="${DENEB_DEPLOY_REMOTE_DIR:-deneb}" # remote $HOME-relative repo dir
LOG_FILE="/tmp/deneb-gateway.log"
LOG_ARCHIVE_DIR="/tmp/deneb-gateway-logs"
LOG_ARCHIVE_KEEP=20   # keep last N pre-restart logs; older ones get pruned
LOG_ARCHIVE_MAX_BYTES=$((200 * 1024 * 1024))  # cap archive dir at 200MB

health_ok() {
    # Auto-detect listen address — gateway may bind loopback OR a specific
    # interface (e.g. tailnet) depending on --bind. ss output col 4 is
    # "Local Address:Port".
    local listen addr
    listen=$(ss -ltnH "sport = :$PROD_PORT" 2>/dev/null | awk '{print $4}' | head -1)
    [[ -z "$listen" ]] && return 1
    case "$listen" in
        "*:"*|"0.0.0.0:"*|"[::]:"*) addr="127.0.0.1:$PROD_PORT" ;;
        *)                          addr="$listen" ;;
    esac
    curl -sf "http://$addr/health" > /dev/null
}

systemd_unit_loaded() {
    command -v systemctl >/dev/null 2>&1 \
        && systemctl --user show "$GATEWAY_SERVICE" -p LoadState --value 2>/dev/null | grep -qx loaded
}

systemd_main_pid() {
    systemctl --user show "$GATEWAY_SERVICE" -p MainPID --value 2>/dev/null || echo 0
}

wait_for_systemd_health() {
    local before_pid="${1:-0}"
    local deadline=$((SECONDS + RESTART_WAIT_SEC))
    local pid=""

    while (( SECONDS < deadline )); do
        if systemctl --user is-active --quiet "$GATEWAY_SERVICE"; then
            pid="$(systemd_main_pid)"
            if [[ -n "$pid" && "$pid" != "0" && "$pid" != "$before_pid" ]] && health_ok; then
                return 0
            fi
        fi
        sleep 1
    done
    return 1
}

restart_with_systemd() {
    local before_pid="0"
    before_pid="$(systemd_main_pid)"

    if systemctl --user is-active --quiet "$GATEWAY_SERVICE"; then
        # Turn-aware swap: wait (bounded) for in-flight agent turns to finish so
        # the graceful drain — which is downtime for new requests — is instant
        # instead of minutes. Timeout proceeds anyway; never blocks a deploy.
        "$(dirname "${BASH_SOURCE[0]}")/wait-idle.sh" "$PROD_PORT" "${DENEB_DEPLOY_IDLE_WAIT_SEC:-420}" || true
        echo "==> hot restarting $GATEWAY_SERVICE with SIGUSR1 (old pid $before_pid)"
        if ! systemctl --user kill --kill-who=main -s SIGUSR1 "$GATEWAY_SERVICE"; then
            echo "    SIGUSR1 failed; falling back to systemctl restart"
            systemctl --user restart "$GATEWAY_SERVICE"
        fi
    else
        echo "==> starting inactive $GATEWAY_SERVICE"
        systemctl --user start "$GATEWAY_SERVICE"
    fi

    if wait_for_systemd_health "$before_pid"; then
        echo "==> deploy OK ($GATEWAY_SERVICE, pid $(systemd_main_pid), port $PROD_PORT)"
        return 0
    fi

    # If the OLD pid is still alive and healthy, the gateway most likely
    # REFUSED the cutover (downgrade guard) — a fallback `systemctl restart`
    # would kill the good process and boot the refused stale binary at the
    # ExecStart path with no guard running. Stop here instead.
    if [[ -n "$before_pid" && "$before_pid" != "0" ]] && kill -0 "$before_pid" 2>/dev/null && health_ok; then
        echo "ERROR: old gateway (pid $before_pid) is still serving — SIGUSR1 was likely REFUSED by the downgrade guard. NOT falling back to a hard restart (it would boot the refused binary). journalctl에서 'restart REFUSED'를 확인하세요; 의도적 롤백은 DENEB_DEPLOY_FORCE=1." >&2
        return 1
    fi
    echo "WARN: health check after SIGUSR1/start failed; trying one direct restart" >&2
    systemctl --user restart "$GATEWAY_SERVICE"
    if wait_for_systemd_health 0; then
        echo "==> deploy OK ($GATEWAY_SERVICE, pid $(systemd_main_pid), port $PROD_PORT)"
        return 0
    fi

    echo "ERROR: gateway service did not become healthy on :$PROD_PORT" >&2
    systemctl --user status "$GATEWAY_SERVICE" --no-pager || true
    return 1
}

restart_with_nohup() {
    # Restart — graceful first, then SIGKILL only after the drain/watchdog window.
    # This gives active agent runs a chance to finish instead of being killed
    # mid-turn, which otherwise leaves replies half-delivered to the native client.
    echo "==> restarting gateway with nohup fallback (port $PROD_PORT)"

    # Prefer port-based detection so we catch both the built binary AND any
    # `go run` instance that was started manually (whose cmdline path lives
    # under /tmp/go-build... and does not contain "deneb-gateway").
    existing_pid=$(ss -ltnpH "sport = :$PROD_PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -1 || true)
    if [[ -z "$existing_pid" ]]; then
        existing_pid=$(pgrep -f 'dist/deneb-gateway' || true)
    fi
    if [[ -n "$existing_pid" ]]; then
        echo "    graceful SIGTERM -> pid $existing_pid (up to ${RESTART_WAIT_SEC}s drain)"
        kill -TERM "$existing_pid" 2>/dev/null || true
        for _ in $(seq 1 "$RESTART_WAIT_SEC"); do
            if ! kill -0 "$existing_pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done
        if kill -0 "$existing_pid" 2>/dev/null; then
            echo "    still alive after ${RESTART_WAIT_SEC}s -> SIGKILL"
            kill -KILL "$existing_pid" 2>/dev/null || true
            sleep 1
        fi
    fi

    # Rotate the previous log before starting the new gateway. Truncating
    # (`>`) on every restart lost the entire pre-restart log, so postmortems
    # of "what happened just before the restart" had nothing to work with.
    if [[ -s "$LOG_FILE" ]]; then
        mkdir -p "$LOG_ARCHIVE_DIR"
        stamp=$(date +%Y%m%d-%H%M%S)
        mv "$LOG_FILE" "$LOG_ARCHIVE_DIR/deneb-gateway-$stamp.log"
        (
            gzip -f "$LOG_ARCHIVE_DIR/deneb-gateway-$stamp.log" 2>/dev/null || true
        ) &
    fi

    # Prune archive: keep the newest LOG_ARCHIVE_KEEP files AND respect the
    # total-size cap.
    if [[ -d "$LOG_ARCHIVE_DIR" ]]; then
        # shellcheck disable=SC2012
        ls -t "$LOG_ARCHIVE_DIR"/deneb-gateway-*.log* 2>/dev/null \
            | tail -n +$((LOG_ARCHIVE_KEEP + 1)) \
            | xargs -r rm -f
        while :; do
            total=$(du -sb "$LOG_ARCHIVE_DIR" 2>/dev/null | awk '{print $1+0}')
            [[ -z "$total" || "$total" -le "$LOG_ARCHIVE_MAX_BYTES" ]] && break
            # shellcheck disable=SC2012
            oldest=$(ls -tr "$LOG_ARCHIVE_DIR"/deneb-gateway-*.log* 2>/dev/null | head -n 1)
            [[ -z "$oldest" ]] && break
            rm -f "$oldest"
        done
    fi

    nohup ./dist/deneb-gateway --bind loopback --port "$PROD_PORT" >> "$LOG_FILE" 2>&1 &

    sleep 2
    if health_ok; then
        echo "==> deploy OK (pid $(pgrep -f deneb-gateway), port $PROD_PORT)"
    else
        echo "ERROR: gateway not responding on :$PROD_PORT" >&2
        tail -20 "$LOG_FILE"
        exit 1
    fi
}

# restart_remote ships the locally-built binary to a remote gateway host and
# hot-swaps it: back up the current binary, atomically replace it (the running
# process keeps its old inode until it exits), then SIGUSR1 → the gateway exits
# with code 75 → systemd `Restart=always` relaunches the new binary at the dist
# path. RefuseManualStop only blocks `systemctl stop`, not signals, so this is
# the supported cutover. Poll for a fresh MainPID + healthy /health before
# declaring success.
restart_remote() {
    local remote="$DEPLOY_REMOTE" dir="$DEPLOY_REMOTE_DIR" bin="dist/deneb-gateway"
    if [[ ! -x "$bin" ]]; then
        echo "ERROR: built binary $bin missing" >&2
        exit 1
    fi
    echo "==> remote deploy → $remote:~/$dir/dist (build host $(hostname))"
    scp -q "$bin" "$remote:$dir/dist/deneb-gateway.new"
    # PHASE 1 — sender-side downgrade gate on the staged .new, BEFORE anything
    # else mutates the host (previously the gate ran after the skills rsync had
    # already --delete-mirrored the build host's tree, so a refused stale deploy
    # still left prod's skills catalog rewritten). The receiver's SIGUSR1 guard
    # is the real wall — this fails fast with a clear message. A candidate that
    # cannot answer --print-version is a pre-guard (stale) build: same handling,
    # or a forced rollback to an old build would never mint the marker and a
    # non-forced one would only fail after the restart poll.
    # shellcheck disable=SC2029 # values intentionally expand on the sender
    ssh "$remote" "PROD_PORT='$PROD_PORT' DIR='$dir' DENEB_DEPLOY_FORCE='${DENEB_DEPLOY_FORCE:-}' bash -s" <<'GATE'
set -euo pipefail
cd "$HOME/$DIR/dist"
chmod +x deneb-gateway.new 2>/dev/null || true
oldver=$(curl -sf -m 3 "http://127.0.0.1:$PROD_PORT/health" | tr ',' '\n' | grep '"version"' | head -1 | cut -d'"' -f4 || true)
candout=$(./deneb-gateway.new --print-version 2>/dev/null || true)
candver=$(printf '%s' "$candout" | awk '{print $1}')
downgrade=""
if [ -n "${oldver:-}" ]; then
    if [ -z "${candver:-}" ]; then
        downgrade="1" # version-less candidate = pre-guard build
        candver="(no --print-version)"
    elif [ "$candver" != "$oldver" ]; then
        lower=$(printf '%s\n%s\n' "$oldver" "$candver" | sort -V | head -1)
        [ "$lower" = "$candver" ] && downgrade="1"
    fi
fi
if [ -n "$downgrade" ]; then
    if [ "${DENEB_DEPLOY_FORCE:-}" = "1" ]; then
        touch .allow-downgrade
        echo "    ⚠ FORCED downgrade $oldver → $candver (.allow-downgrade 마커 설정 — 수신측 가드 1회 통과)" >&2
    else
        rm -f deneb-gateway.new
        echo "ERROR: candidate version ($candver) is OLDER than running ($oldver) — stale checkout? 의도적 롤백은 DENEB_DEPLOY_FORCE=1" >&2
        exit 1
    fi
fi
GATE
    # Ship runtime-read repo files the gateway discovers from disk — the bundled
    # skills/ catalog — which the binary alone does NOT carry. Without this the
    # lean gateway host serves a frozen catalog: skills added after the host's
    # last full sync never reach it, so they never appear in the native Settings
    # tab (the symptom that exposed this gap on 2026-06-22 — 5 merged skills were
    # invisible because only the binary was shipped). Mirror the repo's skills/
    # so new skills are in place before the new binary starts and rediscovers the
    # catalog. --delete keeps it a true mirror (agent-authored skills live under
    # the state dir ~/.deneb, not here, so nothing local is at risk). Runs only
    # after the gate passes so a refused deploy leaves the catalog untouched.
    echo "    syncing skills/ → $remote:~/$dir/skills"
    rsync -a --delete skills/ "$remote:$dir/skills/"
    # PHASE 2 — cutover.
    # shellcheck disable=SC2029 # values intentionally expand on the sender
    ssh "$remote" "GATEWAY_SERVICE='$GATEWAY_SERVICE' PROD_PORT='$PROD_PORT' DIR='$dir' RESTART_WAIT_SEC='$RESTART_WAIT_SEC' bash -s" <<'REMOTE'
set -euo pipefail
cd "$HOME/$DIR/dist"
cp -p deneb-gateway deneb-gateway.bak-prev 2>/dev/null || true
mv deneb-gateway.new deneb-gateway
oldpid=$(systemctl --user show "$GATEWAY_SERVICE" -p MainPID --value 2>/dev/null || true)
[ -z "${oldpid:-}" ] && oldpid=$(pgrep -f 'dist/deneb-gateway' | head -1 || true)
[ -z "${oldpid:-}" ] && { echo "ERROR: no running gateway to cut over" >&2; exit 1; }
oldver=$(curl -sf -m 3 "http://127.0.0.1:$PROD_PORT/health" | tr ',' '\n' | grep '"version"' | head -1 | cut -d'"' -f4 || true)
echo "    SIGUSR1 → pid $oldpid (cutover, old version ${oldver:-unknown})"
kill -USR1 "$oldpid"
for i in $(seq 1 "$RESTART_WAIT_SEC"); do
    pid=$(systemctl --user show "$GATEWAY_SERVICE" -p MainPID --value 2>/dev/null || true)
    [ -z "${pid:-}" ] && pid=$(pgrep -f 'dist/deneb-gateway' | head -1 || true)
    if [ -n "${pid:-}" ] && [ "$pid" != "$oldpid" ] && curl -sf -o /dev/null "http://127.0.0.1:$PROD_PORT/health"; then
        newver=$(curl -sf -m 3 "http://127.0.0.1:$PROD_PORT/health" | tr ',' '\n' | grep '"version"' | head -1 | cut -d'"' -f4 || true)
        echo "    remote deploy OK: new pid $pid after ${i}s (version ${newver:-unknown})"
        # Version-regression tripwire: on 2026-07-04 23:17 an unattributed deploy
        # cut production over to a stale-tag build (4.61.x → 4.47.1) and every
        # native-app data screen went dark until the next morning. A deploy that
        # LOWERS the reported version is almost always a stale checkout — scream,
        # don't stop (the operator may be rolling back on purpose).
        if [ -n "${oldver:-}" ] && [ -n "${newver:-}" ] && [ "$newver" != "$oldver" ]; then
            lower=$(printf '%s
%s
' "$oldver" "$newver" | sort -V | head -1)
            if [ "$lower" = "$newver" ]; then
                echo "    ⚠ WARNING: version went BACKWARD ($oldver → $newver) — stale checkout? verify your build host's git tags" >&2
            fi
        fi
        exit 0
    fi
    sleep 1
done
echo "ERROR: remote gateway unhealthy after cutover (rollback: mv deneb-gateway.bak-prev deneb-gateway && kill -USR1)" >&2
exit 1
REMOTE
}

cd "$PROD_DIR"

# Ensure we're on main
branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
    echo "ERROR: production must be on main (currently on $branch)" >&2
    exit 1
fi

# Pull latest. Force a non-rebase fast-forward regardless of the checkout's
# pull.* config: a box with pull.rebase=true (and especially with pull.ff=only
# also set) otherwise dies here with "Cannot rebase onto multiple branches",
# even though production only ever fast-forwards main. -c overrides for this
# invocation only; it does not touch the repo's stored config.
echo "==> git pull"
git -c pull.rebase=false pull --ff-only origin main

# Build
echo "==> make gateway-prod"
make gateway-prod

# Amaranth reader (Playwright) — node_modules is gitignored; keep prod in sync
# so miniapp.groupware.* / radar don't fail with ERR_MODULE_NOT_FOUND.
if [[ -f scripts/dev/groupware-reader/package-lock.json ]]; then
    # Node lives in user space (~/.local/bin symlinks into the ~/node-sdk
    # tarball), which the systemd timer's fixed PATH lacks — same trap as the
    # Go/cargo toolchains on this host. Prepend the bin dir instead of
    # resolving an absolute npm: npm's own shebang (#!/usr/bin/env node)
    # needs node on PATH too.
    if ! command -v npm >/dev/null 2>&1; then
        for node_bin in "$HOME/.local/bin" "$HOME"/node-sdk/node-*/bin; do
            if [[ -x "$node_bin/npm" ]]; then
                PATH="$node_bin:$PATH"
                break
            fi
        done
    fi
    if command -v npm >/dev/null 2>&1; then
        echo "==> groupware-reader npm ci"
        (cd scripts/dev/groupware-reader && npm ci --omit=dev) || \
            echo "WARN: groupware-reader npm ci failed (전자결재 RPC may be broken)" >&2
    else
        echo "WARN: npm not found on PATH, ~/.local/bin, or ~/node-sdk — skipping groupware-reader npm ci (전자결재 RPC may be broken)" >&2
    fi
fi

if [[ "${1:-}" == "--build-only" ]]; then
    echo "==> build done (--build-only, skipping restart)"
    exit 0
fi

# Remote topology (srv4): built here, ship + hot-swap there. Bypasses the
# in-place restart modes below.
if [[ -n "$DEPLOY_REMOTE" ]]; then
    restart_remote
    exit 0
fi

# Refresh the CodeGraph index so the runtime agent's self-inspection tools
# (codegraph_* via DENEB_MCP_SERVERS) map the code we're about to run. Only
# meaningful on the in-place topology, where $PROD_DIR is the gateway host's
# own checkout — so it lives here, past the remote-topology exit. Runs before
# the restart: the fresh MCP child then opens an up-to-date index (the old,
# --no-watch child is idle and never writes, so a concurrent sync is safe).
#
# Fully soft: gated on codegraph being installed (operator/self-provisioned),
# and never allowed to fail the deploy — a broken index is a degraded
# self-inspection surface, not a reason to block production. AST-only, seconds.
if command -v codegraph >/dev/null 2>&1; then
    echo "==> codegraph index refresh"
    if [[ -d .codegraph ]]; then
        codegraph sync . || echo "    codegraph sync failed (non-fatal); serving prior index"
    else
        codegraph init . || echo "    codegraph init failed (non-fatal); self-inspection tools will be absent"
    fi
fi

# Refresh the semantic code index (the runtime `code_search` tool) right after
# the CodeGraph sync above, so concept search reflects the code we're about to
# serve. Incremental: only nodes whose CodeGraph updated_at changed re-embed, so
# a typical deploy re-embeds a handful (seconds); the first-ever run embeds the
# full node set (~90s) and creates .codegraph/semantic-code.*. Fully soft: gated
# on the embedder sidecar being up, never fails the deploy. Built here via the
# same toolchain that just ran `make gateway-prod` — no separate binary needed.
if [[ -d .codegraph && -f .codegraph/codegraph.db ]] \
    && curl -sf -m 2 "${DENEB_EMBEDDING_URL:-http://127.0.0.1:8002}/health" >/dev/null 2>&1; then
    echo "==> code_search semantic index refresh"
    ( cd gateway-go && go run ./cmd/codesearch index ) >/dev/null 2>&1 \
        || echo "    codesearch index failed (non-fatal); code_search serves prior index or guidance"
fi

case "$RESTART_MODE" in
    systemd)
        restart_with_systemd
        ;;
    nohup)
        restart_with_nohup
        ;;
    auto)
        if systemd_unit_loaded; then
            restart_with_systemd
        else
            restart_with_nohup
        fi
        ;;
    *)
        echo "ERROR: unknown DENEB_DEPLOY_RESTART_MODE=$RESTART_MODE (want auto, systemd, or nohup)" >&2
        exit 1
        ;;
esac
