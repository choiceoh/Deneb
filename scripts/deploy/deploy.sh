#!/usr/bin/env bash
# deploy.sh — Pull latest main and restart production gateway.
# Usage: scripts/deploy/deploy.sh [--build-only]
set -euo pipefail

PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
PROD_PORT="${DENEB_GATEWAY_PORT:-18789}"
GATEWAY_SERVICE="${DENEB_GATEWAY_SERVICE:-deneb-gateway.service}"
RESTART_MODE="${DENEB_DEPLOY_RESTART_MODE:-auto}" # auto | systemd | nohup
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
    local deadline=$((SECONDS + 90))
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
    # Restart — graceful first (SIGTERM, up to 10s), then SIGKILL as fallback.
    # This gives active agent runs a chance to finish instead of being killed
    # mid-turn, which otherwise leaves half-delivered replies in Telegram.
    echo "==> restarting gateway with nohup fallback (port $PROD_PORT)"

    # Prefer port-based detection so we catch both the built binary AND any
    # `go run` instance that was started manually (whose cmdline path lives
    # under /tmp/go-build... and does not contain "deneb-gateway").
    existing_pid=$(ss -ltnpH "sport = :$PROD_PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -1 || true)
    if [[ -z "$existing_pid" ]]; then
        existing_pid=$(pgrep -f 'dist/deneb-gateway' || true)
    fi
    if [[ -n "$existing_pid" ]]; then
        echo "    graceful SIGTERM -> pid $existing_pid (up to 10s drain)"
        kill -TERM "$existing_pid" 2>/dev/null || true
        for _ in 1 2 3 4 5 6 7 8 9 10; do
            if ! kill -0 "$existing_pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done
        if kill -0 "$existing_pid" 2>/dev/null; then
            echo "    still alive after 10s -> SIGKILL"
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

if [[ "${1:-}" == "--build-only" ]]; then
    echo "==> build done (--build-only, skipping restart)"
    exit 0
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
