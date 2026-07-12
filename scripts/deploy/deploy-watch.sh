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
LOCK_FILE="/tmp/deneb-deploy-watch.lock"
LOG_FILE="/tmp/deneb-deploy-watch.log"
GATEWAY_SERVICE="${DENEB_GATEWAY_SERVICE:-deneb-gateway.service}"
PROD_PORT="${DENEB_PROD_PORT:-18789}"

WATCH_SEC="${DENEB_DEPLOY_WATCH_SEC:-600}"
POLL_SEC="${DENEB_DEPLOY_WATCH_POLL_SEC:-30}"
# Journal ERROR-line budget over the whole watch window. The gateway logs
# operational warnings routinely; genuine regressions show up as repeated
# ERROR lines (panic recoveries, failed subsystems), so the budget is loose.
ERROR_BUDGET="${DENEB_DEPLOY_WATCH_ERROR_BUDGET:-30}"
HEALTH_FAILS_TO_ROLLBACK=2

DEPLOYED_HEAD="${1:-}"

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

health_ok() {
    curl -sf --max-time 5 "http://127.0.0.1:$PROD_PORT/health" > /dev/null
}

journal_errors_since() {
    local since="$1"
    journalctl --user -u "$GATEWAY_SERVICE" --since "$since" --no-pager 2>/dev/null \
        | grep -cE ' ERROR | level=ERROR |"level":"error"' || true
}

rollback() {
    local reason="$1"
    local prev_head=""
    [[ -f "$PREV_HEAD_FILE" ]] && prev_head=$(tr -d '[:space:]' < "$PREV_HEAD_FILE")

    log "REGRESSION on head ${DEPLOYED_HEAD:0:10}: $reason — rolling back binary"
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
    log "rollback OK: binary restored (head record → ${prev_head:0:10}); ${DEPLOYED_HEAD:0:10} blocked until a newer commit lands"
    return 0
}

main() {
    exec 9>"$LOCK_FILE"
    if ! flock -n 9; then
        log "another watch is active; deferring to it (head ${DEPLOYED_HEAD:0:10} unwatched)"
        exit 0
    fi

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
    log "watch window clear for ${DEPLOYED_HEAD:0:10} (errors=$(journal_errors_since "$started_at"))"
    exit 0
}

main "$@"
