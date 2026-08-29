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
# Thresholds sit against measured normals (2026-08-29, post 200G + fan re-tune):
# cpu 46-49C, switch-chip 63C, board 38-43C, fans 7.4-7.8K RPM, psu1 41W. The
# chip's overtemp shutdown is 115C, so 85C is a wide early warning.
#
# The older 3.9-4.8K RPM baseline (2026-08-09) is no longer reachable: the 08-09
# 200G cutover (two ports at 200G-baseCR4) raised psu draw 37.7W -> 41W+, and
# holding the chip at 63C now costs ~7.5K RPM. Do not treat 4.8K as the target.
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
# Sustained high RPM = the fan-control oscillation (2026-08-29). Root cause is a
# ZERO proportional band: fan-target-temp == fan-full-speed-temp (both 65C) makes
# the controller bang-bang — idle below the threshold, 100% the instant it is
# touched. That stayed hidden while the chip sat at 62C and never reached 65C;
# after the 200G cutover it does, and the loop hunts between 5K and 13.8K.
#
# Do NOT "fix" loud fans by raising fan-target-temp to 65C — that CREATES the
# oscillation (an 08-29 morning attempt did exactly that and made it worse).
# The fix is a proportional band: keep full-speed at 65C and set target BELOW it
# (64C in production -> steady 7.4-7.8K at 63C). Widening upward is impossible;
# both knobs cap at 65C (`out of range (-273..65)`).
#
# Signature to distinguish: in the 30-minute samples below, oscillation shows RPM
# INVERSELY correlated with temperature (a high-RPM sample is the moment just
# after a full-speed slam dropped the temp). Real high load reads the other way.
# Tuned-normal is 7.4-7.8K; the 9K bound catches a return to hunting.
FAN_RPM_MAX="${CRS812_FAN_RPM_MAX:-9000}"

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
    [[ -n "$rpm" && "$rpm" -gt "$FAN_RPM_MAX" ]] && faults+=("${fan} ${rpm}RPM 과속 — 팬 제어 발진 의심 (상한 ${FAN_RPM_MAX}). target=full-speed=65C면 비례 구간 0 → target을 64C로. 65C로 올리지 말 것")
done

cpu_temp="$(value_of cpu-temperature)"
[[ -n "$cpu_temp" && "$cpu_temp" -ge "$CPU_TEMP_MAX" ]] && faults+=("CPU ${cpu_temp}°C (임계 ${CPU_TEMP_MAX})")
switch_temp="$(value_of switch-temperature)"
[[ -n "$switch_temp" && "$switch_temp" -ge "$SWITCH_TEMP_MAX" ]] && faults+=("스위치칩 ${switch_temp}°C (임계 ${SWITCH_TEMP_MAX})")

# Always leave a one-line trend sample in the journal (fault or not): the
# 08-29 "yesterday it was quiet" episode had no way to answer "what changed
# and when" — 30-minute temp/RPM samples make the next step-change datable.
echo "crs812-health-watch: sample switch=${switch_temp:-?}C cpu=${cpu_temp:-?}C fan=$(value_of fan1-speed)/$(value_of fan2-speed)/$(value_of fan3-speed)/$(value_of fan4-speed)rpm psu=$(value_of psu1-power)W" >&2

if ((${#faults[@]} == 0)); then
    exit 0
fi

printf -v body '%s · ' "${faults[@]}"
alert bad "스위치 하드웨어 이상" "${body%· }"
echo "crs812-health-watch: ${body%· }" >&2
exit 0
