#!/usr/bin/env bash
# wait-idle.sh — deploy idle gate: wait (bounded) until the gateway reports no
# in-flight agent turns before a hot-swap.
#
# Why: the gateway's graceful drain lets a RUNNING turn finish before the old
# process exits — but the whole drain is downtime for NEW requests. Swapping
# while a turn is active turns a ~2s restart into minutes of refused
# connections (and the 2026-06-10 incident showed capped drains still abort
# cron agent turns). Waiting for idle FIRST makes the drain instant, so the
# outage window collapses to boot time.
#
# Usage: wait-idle.sh [PORT] [TIMEOUT_SEC]
#   PORT        gateway port to poll (default 18789)
#   TIMEOUT_SEC max wait (default 420 — above the 5-minute turn deadline, so a
#               single in-flight turn always finishes naturally within the gate;
#               0 disables the gate entirely)
#
# Exit codes: 0 = idle observed; 1 = timed out (caller proceeds either way —
# the gate trades a bounded delay for a shorter outage, never blocks a deploy).
# Backward compatible: an old binary without the activity field reads as idle.
set -u

PORT="${1:-18789}"
TIMEOUT_SEC="${2:-420}"
POLL_SEC="${DENEB_DEPLOY_IDLE_POLL_SEC:-15}"

active_turns() {
    curl -sf --max-time 5 "http://127.0.0.1:$PORT/health" 2>/dev/null | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("activity", {}).get("active_turns", 0))
except Exception:
    print(0)
' 2>/dev/null || echo 0
}

if (( TIMEOUT_SEC <= 0 )); then
    exit 0
fi

deadline=$(( $(date +%s) + TIMEOUT_SEC ))
while :; do
    turns="$(active_turns)"
    if [[ "$turns" == "0" ]]; then
        exit 0
    fi
    now=$(date +%s)
    if (( now >= deadline )); then
        echo "wait-idle: timed out after ${TIMEOUT_SEC}s with $turns active turn(s) — proceeding" >&2
        exit 1
    fi
    echo "wait-idle: $turns active turn(s) on :$PORT — waiting (deadline in $(( deadline - now ))s)" >&2
    sleep "$POLL_SEC"
done
