#!/usr/bin/env bash
# deploy-watch.sh — post-hot-swap rollback watch (RSI L4 auto-apply의 전제).
#
# auto-deploy.sh가 "deploy OK" 직후 백그라운드로 발사한다. WATCH_SEC 동안
# 게이트웨이를 감시해서 (a) /health 연속 실패 또는 (b) 저널 ERROR 폭주가
# 보이면 직전 바이너리(deneb-gateway.bak-prev)로 복원 + 재시작하고, 회귀한
# 커밋을 REGRESS_FILE에 기록해 auto-deploy가 같은 head를 재배포하지 않게
# 막는다(더 새로운 커밋이 landing되면 remote head가 달라져 자동 해제).
#
# 설계 원칙 (auto-deploy.sh와 동일):
# - 항상 exit 0. 실패는 로그로만 (/tmp/deneb-deploy-watch.log).
# - flock 단일 인스턴스. 새 배포가 뜨면 STATE_FILE head가 달라지므로 이전
#   워치는 다음 폴에서 스스로 물러난다.
# - 롤백은 바이너리 수준(git 무접촉): dist/deneb-gateway.bak-prev 복원 +
#   systemctl restart. 다운그레이드 가드는 SIGUSR1 경로에만 있으므로 hard
#   restart가 복원 바이너리를 그대로 부팅한다.
set -euo pipefail

PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
STATE_FILE="$STATE_DIR/auto-deploy.deployed-head"
PREV_HEAD_FILE="$STATE_DIR/auto-deploy.prev-head"
REGRESS_FILE="$STATE_DIR/auto-deploy.regressed-head"
LOCK_FILE="${DENEB_DEPLOY_WATCH_LOCK_FILE:-/tmp/deneb-deploy-watch.lock}"
LOG_FILE="${DENEB_DEPLOY_WATCH_LOG_FILE:-/tmp/deneb-deploy-watch.log}"
READY_FILE="${DENEB_DEPLOY_WATCH_READY_FILE:-$STATE_DIR/deploy-watch.ready}"
GATEWAY_SERVICE="${DENEB_GATEWAY_SERVICE:-deneb-gateway.service}"
PROD_PORT="${DENEB_PROD_PORT:-18789}"

WATCH_SEC="${DENEB_DEPLOY_WATCH_SEC:-600}"
POLL_SEC="${DENEB_DEPLOY_WATCH_POLL_SEC:-30}"
HANDOFF_SEC="${DENEB_DEPLOY_WATCH_HANDOFF_SEC:-$((POLL_SEC * 2 + 15))}"
# Journal ERROR-line budget over the whole watch window. The gateway logs
# operational warnings routinely; genuine regressions show up as repeated
# ERROR lines (panic recoveries, failed subsystems), so the budget is loose.
ERROR_BUDGET="${DENEB_DEPLOY_WATCH_ERROR_BUDGET:-30}"
HEALTH_FAILS_TO_ROLLBACK=2

DEPLOYED_HEAD="${1:-}"
DISPATCH_RPC="$PROD_DIR/scripts/dev/self_correction_dispatch.py"
TRACKED_IDS=()
TRACKED_ATTEMPTS=()
TRACKED_HEADS=()

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

mark_ready() {
    local tmp="$READY_FILE.tmp.$$"
    mkdir -p "$(dirname "$READY_FILE")"
    printf '%s %s %s\n' "$DEPLOYED_HEAD" "$$" "$(date +%s)" > "$tmp"
    mv -f "$tmp" "$READY_FILE"
}

health_ok() {
    curl -sf --max-time 5 "http://127.0.0.1:$PROD_PORT/health" > /dev/null
}

journal_errors_since() {
    local since="$1"
    journalctl --user -u "$GATEWAY_SERVICE" --since "$since" --no-pager 2>/dev/null \
        | grep -cE ' ERROR | level=ERROR |"level":"error"' || true
}

record_dispatch_event() {
    python3 "$DISPATCH_RPC" --state-dir "$STATE_DIR" record "$@" >>"$LOG_FILE" 2>&1
}

# Carry every unresolved dispatch whose merged commit is included in this head.
# "deployed" rows are inherited from a superseded watcher; "merged" rows cross
# the deploy boundary here for the first time.
#
# Per-candidate deploy head: the tracker's provenance guard pins deployHead per
# attempt, so an INHERITED candidate (deployed under an earlier head, its watch
# superseded by this one) must be recorded under ITS OWN recorded head — using
# this watcher's head conflicted and permanently lost the watch_passed ledger
# event on every rapid-deploy day (observed 2026-07-16: 8 deploys, events lost).
track_dispatch_candidates() {
    local cid attempt phase commit_sha prior_head
    [[ -f "$DISPATCH_RPC" ]] || return 0
    while IFS=$'\t' read -r cid attempt phase commit_sha prior_head; do
        [[ -n "$cid" && -n "$attempt" && -n "$commit_sha" ]] || continue
        if ! git -C "$PROD_DIR" merge-base --is-ancestor "$commit_sha" "$DEPLOYED_HEAD" 2>/dev/null; then
            continue
        fi
        TRACKED_IDS+=("$cid")
        TRACKED_ATTEMPTS+=("$attempt")
        if [[ "$phase" == "merged" ]]; then
            TRACKED_HEADS+=("$DEPLOYED_HEAD")
            record_dispatch_event --id "$cid" --phase deployed --attempt-id "$attempt" \
                --commit-sha "$commit_sha" --deploy-head "$DEPLOYED_HEAD" \
                --note "candidate included in deployed main head" \
                || log "WARN: failed to record deployed event for $cid"
        else
            TRACKED_HEADS+=("${prior_head:-$DEPLOYED_HEAD}")
        fi
    done < <(python3 "$DISPATCH_RPC" --state-dir "$STATE_DIR" list \
        --phase merged --phase deployed 2>>"$LOG_FILE" || true)
    if (( ${#TRACKED_IDS[@]} > 0 )); then
        log "tracking ${#TRACKED_IDS[@]} self-correction dispatch(es) in ${DEPLOYED_HEAD:0:10}"
    fi
}

record_tracked_candidates() {
    local phase="$1" note="$2" i head
    [[ -f "$DISPATCH_RPC" ]] || return 0
    for i in "${!TRACKED_IDS[@]}"; do
        # The candidate's OWN deploy head (see track_dispatch_candidates) — the
        # provenance guard rejects any other value for an inherited candidate.
        head="${TRACKED_HEADS[$i]:-$DEPLOYED_HEAD}"
        record_dispatch_event --id "${TRACKED_IDS[$i]}" --phase "$phase" \
            --attempt-id "${TRACKED_ATTEMPTS[$i]}" --deploy-head "$head" --note "$note" \
            || log "WARN: failed to record $phase event for ${TRACKED_IDS[$i]}"
    done
}

rollback() {
    local reason="$1"
    local prev_head=""
    [[ -f "$PREV_HEAD_FILE" ]] && prev_head=$(tr -d '[:space:]' < "$PREV_HEAD_FILE")

    log "REGRESSION on head ${DEPLOYED_HEAD:0:10}: $reason — rolling back binary"
    # Rollback ledger: the graduation ladder's dispatch-cap row requires "0
    # deploy-watch rollbacks" as machine-readable evidence (genesis
    # deployWatchRollbacks reads this file). Append-only, best-effort.
    printf '{"ts":%s,"event":"rollback","head":"%s","reason":%s}\n' \
        "$(date +%s%3N)" "$DEPLOYED_HEAD" "$(printf '%s' "$reason" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')" \
        >> "$HOME/.deneb/data/deploy_watch_log.jsonl" 2>>"$LOG_FILE" || true
    if [[ ! -f "$PROD_DIR/dist/deneb-gateway.bak-prev" ]]; then
        log "ERROR: no previous binary (dist/deneb-gateway.bak-prev) — cannot roll back automatically"
        return 1
    fi
    cp -p "$PROD_DIR/dist/deneb-gateway.bak-prev" "$PROD_DIR/dist/deneb-gateway"
    # The unit sets RefuseManualStop=yes, so `systemctl restart` is REFUSED
    # ("Operation refused", verified live 2026-07-12) — the hard restart goes
    # through Restart=always instead: TERM the tracked main pid and wait for
    # systemd to boot the restored binary under a new pid. Never pkill by
    # pattern; only the exact MainPID.
    local old_pid
    old_pid=$(systemctl --user show -p MainPID --value "$GATEWAY_SERVICE" 2>>"$LOG_FILE")
    if [[ -z "$old_pid" || "$old_pid" == "0" ]] || ! kill -TERM "$old_pid" 2>>"$LOG_FILE"; then
        log "ERROR: cannot signal gateway main pid (${old_pid:-none}) after rollback — manual intervention required"
        return 1
    fi
    local waited=0 new_pid=""
    while (( waited < 90 )); do
        sleep 5
        waited=$(( waited + 5 ))
        new_pid=$(systemctl --user show -p MainPID --value "$GATEWAY_SERVICE" 2>>"$LOG_FILE")
        if [[ -n "$new_pid" && "$new_pid" != "0" && "$new_pid" != "$old_pid" ]] && health_ok; then
            break
        fi
    done
    if [[ -z "$new_pid" || "$new_pid" == "0" || "$new_pid" == "$old_pid" ]] || ! health_ok; then
        log "ERROR: gateway unhealthy even after rollback (pid ${new_pid:-none}) — manual intervention required"
        return 1
    fi
    # Block this head from redeploying until a NEWER commit lands on main
    # (auto-deploy compares remote_head against this file every tick).
    if [[ -n "$DEPLOYED_HEAD" ]]; then
        printf '%s %s\n' "$DEPLOYED_HEAD" "$(date +%s)" > "$REGRESS_FILE"
    fi
    # Reset the deployed-head record so the ledgered state matches reality.
    if [[ -n "$prev_head" ]]; then
        printf '%s\n' "$prev_head" > "$STATE_FILE"
    fi
    record_tracked_candidates rolled_back "$reason; binary restored to ${prev_head:0:10}"
    log "rollback OK: binary restored (head record → ${prev_head:0:10}); ${DEPLOYED_HEAD:0:10} blocked until a newer commit lands"
    return 0
}

main() {
    exec 9>"$LOCK_FILE"
    # Latest-head handoff: the prior watcher sees STATE_FILE change and yields;
    # this watcher waits for that lock instead of exiting and leaving the new
    # deployment unwatched.
    if ! flock -w "$HANDOFF_SEC" 9; then
        log "ERROR: watch handoff timed out for head ${DEPLOYED_HEAD:0:10} after ${HANDOFF_SEC}s"
        exit 0
    fi

    if [[ -z "$DEPLOYED_HEAD" ]]; then
        log "ERROR: deploy-watch requires a deployed head"
        exit 0
    fi
    if [[ -f "$STATE_FILE" ]] && [[ "$(tr -d '[:space:]' < "$STATE_FILE")" != "$DEPLOYED_HEAD" ]]; then
        log "stale watcher acquired lock after a newer deploy; skipping ${DEPLOYED_HEAD:0:10}"
        exit 0
    fi
    mark_ready
    track_dispatch_candidates

    local started_at deadline health_fails=0 errors
    started_at=$(date -Iseconds)
    deadline=$(( $(date +%s) + WATCH_SEC ))
    log "watch started for head ${DEPLOYED_HEAD:0:10} (window ${WATCH_SEC}s, poll ${POLL_SEC}s, error budget $ERROR_BUDGET)"

    while (( $(date +%s) < deadline )); do
        sleep "$POLL_SEC"
        # A newer deploy resets the game — this watch is stale, bow out.
        if [[ -f "$STATE_FILE" ]] && [[ "$(tr -d '[:space:]' < "$STATE_FILE")" != "$DEPLOYED_HEAD" ]]; then
            log "newer deploy detected; ending watch for ${DEPLOYED_HEAD:0:10}"
            exit 0
        fi
        if health_ok; then
            health_fails=0
        else
            health_fails=$(( health_fails + 1 ))
            log "health check failed ($health_fails/$HEALTH_FAILS_TO_ROLLBACK)"
            if (( health_fails >= HEALTH_FAILS_TO_ROLLBACK )); then
                rollback "health failed ${health_fails}x" || true
                exit 0
            fi
            continue
        fi
        errors=$(journal_errors_since "$started_at")
        if (( errors > ERROR_BUDGET )); then
            rollback "journal ERROR flood ($errors > $ERROR_BUDGET in window)" || true
            exit 0
        fi
    done
    errors=$(journal_errors_since "$started_at")
    record_tracked_candidates watch_passed "rollback watch clear; journal errors=$errors"
    log "watch window clear for ${DEPLOYED_HEAD:0:10} (errors=$errors)"
    exit 0
}

main "$@"
