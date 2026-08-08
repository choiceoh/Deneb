#!/usr/bin/env bash
# crs812-health-watch.sh — alert on CRS812 fabric-switch hardware faults.
#
# Why: the switch carries the whole 100G fabric and nothing watches it. It has
# no monitoring of any kind, so a fan seizing or a PSU dropping is invisible
# until the unit overheats or dies. That is not hypothetical — psu2 has read
# `no-input` for as long as anyone has looked (a known single-PSU choice), and
# the same blindness covers psu1, whose loss would take the fabric down.
#
# Faults relay through the gateway's Fleet alert hook (loopback-only
# POST /api/hooks/fleet), which shares the proactive cooldown gate — so a stuck
# fault notifies once rather than every cycle. Read-only against the switch.
#
# Thresholds sit against measured normals (2026-08-09, post fan-tuning):
# cpu 51C, switch-chip 62C, board 36-39C, fans 3.9-4.3K RPM, psu1 37.7W. The
# chip's overtemp shutdown is 115C, so 85C is a wide early warning.
#
# ALWAYS exits 0 (release-and-deploy.md): a red unit invites an operator to
# disable the timer, which would silently end the monitoring this script exists
# to provide. The alert and the journal line are the signal, not unit state.
set -uo pipefail

GATEWAY="${DENEB_GATEWAY_URL:-http://127.0.0.1:18789}"
SWITCH_HOST="${CRS812_HOST:-admin@192.168.88.1}"
SWITCH_VIA="${CRS812_VIA:-srv2}"   # the switch mgmt net is reachable from srv2 only
CPU_TEMP_MAX="${CRS812_CPU_TEMP_MAX:-85}"
SWITCH_TEMP_MAX="${CRS812_SWITCH_TEMP_MAX:-85}"
FAN_RPM_MIN="${CRS812_FAN_RPM_MIN:-1000}"

alert() { # level, title, message
    curl -fsS -m 10 -X POST "$GATEWAY/api/hooks/fleet" \
        -H "Content-Type: application/json" \
        -d "$(printf '{"source":"crs812","level":"%s","title":"%s","message":"%s"}' "$1" "$2" "$3")" \
        >/dev/null 2>&1 || echo "crs812-health-watch: alert relay failed" >&2
}

health="$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SWITCH_VIA" \
    "ssh -o BatchMode=yes -o ConnectTimeout=10 $SWITCH_HOST '/system health print'" 2>/dev/null)"

# An unreachable switch is itself the alert — but only when this host can still
# reach the hop, otherwise a local network blip would page about the switch.
if [[ -z "$health" ]]; then
    if ssh -o BatchMode=yes -o ConnectTimeout=10 "$SWITCH_VIA" true 2>/dev/null; then
        alert bad "스위치 응답 없음" "CRS812(100G 패브릭)가 ${SWITCH_VIA} 경유로 응답하지 않습니다. 패브릭은 아직 살아 있을 수 있으나 관리면이 죽었습니다."
        echo "crs812-health-watch: switch unreachable via $SWITCH_VIA" >&2
    else
        echo "crs812-health-watch: $SWITCH_VIA unreachable; skipping (not a switch fault)" >&2
    fi
    exit 0
fi

# `/system health print` rows are "<n> <name> <value> <type>"; index by name.
value_of() { awk -v k="$1" '$2 == k { print $3 }' <<<"$health"; }

faults=()

fan_state="$(value_of fan-state)"
[[ -n "$fan_state" && "$fan_state" != "ok" ]] && faults+=("팬 상태 ${fan_state}")

# psu2 is deliberately unconnected (single-PSU choice), so only psu1 is a fault.
psu1_state="$(value_of psu1-state)"
[[ -n "$psu1_state" && "$psu1_state" != "ok" ]] && faults+=("PSU1 상태 ${psu1_state} — 단일 전원이라 곧 전체 정지")

for fan in fan1 fan2 fan3 fan4; do
    rpm="$(value_of "${fan}-speed")"
    [[ -n "$rpm" && "$rpm" -lt "$FAN_RPM_MIN" ]] && faults+=("${fan} ${rpm}RPM (하한 ${FAN_RPM_MIN})")
done

cpu_temp="$(value_of cpu-temperature)"
[[ -n "$cpu_temp" && "$cpu_temp" -ge "$CPU_TEMP_MAX" ]] && faults+=("CPU ${cpu_temp}°C (임계 ${CPU_TEMP_MAX})")
switch_temp="$(value_of switch-temperature)"
[[ -n "$switch_temp" && "$switch_temp" -ge "$SWITCH_TEMP_MAX" ]] && faults+=("스위치칩 ${switch_temp}°C (임계 ${SWITCH_TEMP_MAX})")

if ((${#faults[@]} == 0)); then
    exit 0
fi

printf -v body '%s · ' "${faults[@]}"
alert bad "스위치 하드웨어 이상" "${body%· }"
echo "crs812-health-watch: ${body%· }" >&2
exit 0
