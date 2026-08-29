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
# Thresholds sit against measured normals (2026-08-29 evening, post 200G and
# post min-speed floor): cpu 45-49C, switch-chip 58C, board 35C, fans ~5.2K RPM,
# psu1 38.7W. The chip's overtemp shutdown is 115C, so 85C is a wide early
# warning.
#
# An earlier revision of this comment claimed the 3.9-4.8K RPM baseline was "no
# longer reachable" and that holding 63C cost ~7.5K RPM. Both were wrong: those
# figures were sampled mid-oscillation, not at rest. With a floor the fans sit at
# ~5.2K and the chip parks at 58C — below the old quiet baseline's 62C.
#
# psu watts here track fan RPM, not port load (37W at idle spin, 38.7W tuned,
# 49.6W mid-slam), so reading "200G raised draw to 41-50W" as switch load was
# measuring the fans. Base draw with all four links up is still ~38W.
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
# A floor breach now also means the min-speed setting was reverted: with
# fan-min-speed-percent at 40% the fans never idle below ~5K, so anything under
# 3000 says someone put the floor back to 0% (see below) or a fan is failing.
FAN_RPM_MIN="${CRS812_FAN_RPM_MIN:-3000}"
# Sustained high RPM = the fan-control oscillation (2026-08-29). It has TWO
# halves, and fixing only the first leaves the slams in place:
#
#   1. A near-zero proportional band. fan-target-temp == fan-full-speed-temp
#      (both 65C) makes the controller bang-bang. Dropping target to 64C buys a
#      1C band, which is still effectively zero — measured that evening, the
#      chip crossed 64C and then 65C with the fans still parked at their floor.
#   2. No floor. fan-min-speed-percent=0% let the fans idle at ~1.8K, so the
#      chip walked 62C -> 66C in 70s and overshot before the controller reacted.
#      Full cycle: 70s climb, 20s slam to 14,040 RPM, 100s overcooling to 55C,
#      repeating every ~3.5 minutes. That is the swell an operator hears.
#
# The fix is BOTH: target 64C (band) AND fan-min-speed-percent 40% (floor). With
# the floor the chip parks at 58C and never reaches the threshold, so the
# controller never engages and the fans hold a steady ~5.2K.
#
# Do NOT "fix" loud fans by raising fan-target-temp to 65C — that CREATES the
# oscillation (an 08-29 morning attempt did exactly that and made it worse).
# Widening the band upward is impossible; both knobs cap at 65C (`out of range
# (-273..65)`). The floor is the knob with room left.
#
# Measured floor mapping (2026-08-29, idle fabric): 0% -> ~1.8K RPM (chip runs
# away), 35% -> ~4.5K (chip 60C), 40% -> ~5.3K (chip 58C). Step the floor up if
# slams return under summer daytime load.
#
# Signature to distinguish: in the 30-minute samples below, oscillation shows RPM
# INVERSELY correlated with temperature (a high-RPM sample is the moment just
# after a full-speed slam dropped the temp). Real high load reads the other way.
# Tuned-normal is ~5.2K; the 9K bound catches a return to hunting.
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
    [[ -n "$rpm" && "$rpm" -gt "$FAN_RPM_MAX" ]] && faults+=("${fan} ${rpm}RPM 과속 — 팬 제어 발진 의심 (상한 ${FAN_RPM_MAX}). 처방은 둘 다: target=64C(밴드) + fan-min-speed-percent=40%(바닥). 바닥이 0%면 칩이 임계까지 방치돼 슬램이 난다. 65C로 올리지 말 것")
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
