#!/usr/bin/env bash
# Direct iteration loop for Claude Code.
#
# Claude Code runs this to test a code change against the live gateway.
# One-shot: build → start → metric → stop → report.
# Claude Code calls this repeatedly, modifying code between iterations.
#
# Usage:
#   scripts/iterate.sh                    # default: smoke test (3 checks)
#   scripts/iterate.sh --metric CMD       # custom metric command
#   scripts/iterate.sh --port 18791       # custom port (default: 18791)
#   scripts/iterate.sh --baseline         # compare against saved baseline
#   scripts/iterate.sh --save-baseline    # save result as new baseline
#
# Exit code: 0 if metric improved or stable, 1 if degraded or build failed.
#
# Output format (last two lines, machine-readable):
#   ITERATE_RESULT metric=3 build=ok server=ok checks=3/3 latency_ms=147
#   DENEB_TEST_JSON {"version":1,...}

set -euo pipefail

# Source shared dev server library.
source "$(cd "$(dirname "$0")" && pwd)/lib-server.sh"

SCRIPT_DIR="$DEVLIB_SCRIPT_DIR"
REPO_DIR="$DEVLIB_REPO_DIR"
PORT="${ITERATE_PORT:-18791}"
BINARY="/tmp/deneb-gateway-iterate"
LOG="/tmp/deneb-gateway-iterate.log"
HOST="$DEVLIB_HOST"
RESULT_FILE="/tmp/deneb-iterate-result.json"
BUILD_LOG="/tmp/deneb-iterate-build.log"
LOCK_FILE="/tmp/deneb-iterate.lock"
ITERATE_STATE_DIR="/tmp/deneb-iterate-state"

# Parse arguments.
METRIC_CMD=""
METRIC_PRESET=""
USE_BASELINE=false
SAVE_BASELINE=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --metric)
      # Accept preset name (smoke|quality|combined) or raw command.
      case "${2:-}" in
        smoke|quality|combined) METRIC_PRESET="$2"; shift 2 ;;
        *) METRIC_CMD="$2"; shift 2 ;;
      esac
      ;;
    --port) PORT="$2"; shift 2 ;;
    --baseline) USE_BASELINE=true; shift ;;
    --save-baseline) SAVE_BASELINE=true; shift ;;
    --prod-parity) shift ;; # Ignored (prod config is now the default).
    *) shift ;;
  esac
done

# --- Concurrent execution guard ---
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
  echo "BLOCKED: another iterate.sh is running (lock: $LOCK_FILE)"
  echo "ITERATE_RESULT metric=0 build=skip server=skip checks=0/0 latency_ms=0"
  exit 1
fi

# --- JSON result builder ---
# Accumulate result fields; emit at the end.
declare -A PHASE_OK PHASE_MS
CHECKS_JSON="[]"
DIAG_JSON="{}"
QUALITY_JSON="{}"
LOG_ERRORS=0

_json_escape() {
  python3 -c "import json,sys; print(json.dumps(sys.stdin.read().rstrip()))" <<< "$1"
}

_emit_result_json() {
  local commit branch
  commit=$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")
  branch=$(git -C "$REPO_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

  python3 -c "
import json, sys
def tobool(s):
    return s == 'true'
data = {
    'version': 1,
    'commit': '$commit',
    'branch': '$branch',
    'prod_config': True,
    'phase': {
        'build':   {'ok': tobool('${PHASE_OK[build]:-false}'), 'ms': ${PHASE_MS[build]:-0}},
        'start':   {'ok': tobool('${PHASE_OK[start]:-false}'), 'ms': ${PHASE_MS[start]:-0}},
        'test':    {'ok': tobool('${PHASE_OK[test]:-false}'),   'ms': ${PHASE_MS[test]:-0}},
        'cleanup': {'ok': True, 'ms': 0},
    },
    'checks': json.loads('''$CHECKS_JSON'''),
    'quality': json.loads('''$QUALITY_JSON'''),
    'log_errors': $LOG_ERRORS,
    'diagnostics': json.loads('''$DIAG_JSON'''),
}
result = json.dumps(data, ensure_ascii=False)
print(result)
# Also save to file for baseline/other scripts.
with open('$RESULT_FILE', 'w') as f:
    f.write(result)
"
}

# --- Process management (via shared library) ---

cleanup() {
  if [[ -n "${GW_PID:-}" ]]; then
    devlib_stop_pid "$GW_PID"
    GW_PID=""
  fi
  devlib_wait_port_free "$PORT" || {
    local holder
    holder=$(ss -ltnp 2>/dev/null | grep ":$PORT " | head -1 || true)
    echo "  WARN: port $PORT still held: $holder" >&2
  }
}
trap cleanup EXIT

# --- Step 1: Build ---
echo -n "build... "
BUILD_START=$(date +%s%N)
if ! devlib_build "$BINARY" 2>"$BUILD_LOG"; then
  BUILD_MS=$(( ($(date +%s%N) - BUILD_START) / 1000000 ))
  PHASE_OK[build]=false; PHASE_MS[build]=$BUILD_MS
  echo "FAIL (${BUILD_MS}ms)"

  # Extract first compiler error for diagnostics.
  FIRST_ERROR=$(grep -m1 -E '\.go:\d+:\d+:' "$BUILD_LOG" 2>/dev/null || echo "")
  ERROR_COUNT=$(grep -cE '\.go:\d+:\d+:' "$BUILD_LOG" 2>/dev/null || echo "0")

  if [[ -n "$FIRST_ERROR" ]]; then
    echo "  first error: $FIRST_ERROR"
    echo "  total errors: $ERROR_COUNT"
  fi
  echo "  build log: $BUILD_LOG"
  tail -10 "$BUILD_LOG"

  DIAG_JSON=$(python3 -c "
import json
d = {'category': 'build_error', 'error_count': $ERROR_COUNT,
     'first_error': $(python3 -c "import json; print(json.dumps('''$FIRST_ERROR'''.strip()))" 2>/dev/null || echo '""'),
     'log_file': '$BUILD_LOG',
     'suggestion': 'Fix the compilation error in the first_error field, then retry.'}
print(json.dumps(d))
")

  echo "ITERATE_RESULT metric=0 build=fail server=skip checks=0/0 latency_ms=$BUILD_MS"
  echo "DENEB_TEST_JSON $(_emit_result_json)"
  exit 1
fi
BUILD_MS=$(( ($(date +%s%N) - BUILD_START) / 1000000 ))
PHASE_OK[build]=true; PHASE_MS[build]=$BUILD_MS
echo "ok (${BUILD_MS}ms)"

# --- Step 2: Start gateway ---
# Start gateway with production config pointed at the local mock Telegram
# server. The iterate gateway and the live-test dev gateway share the same
# mock instance; they should not be run concurrently (iterate.sh already
# holds an flock for its own run).
echo -n "start... "
DEV_CONFIG="/tmp/deneb-iterate-config.json"
MOCK_TOKEN="mock-dev-token"
DENEB_DEV_TELEGRAM_TOKEN="$MOCK_TOKEN" devlib_gen_config "$DEV_CONFIG"

# Ensure the mock Telegram server is up. Idempotent — no-op if a live-test
# invocation already started it.
devlib_start_mock_telegram "${DENEB_DEV_MOCK_TELEGRAM_PORT:-18792}" "$HOST" >/dev/null 2>&1 || true

START_WAIT_BEGIN=$(date +%s%N)
DENEB_DEV_TELEGRAM_TOKEN="$MOCK_TOKEN" \
  devlib_start_gateway "$BINARY" "$PORT" "$DEV_CONFIG" "$ITERATE_STATE_DIR" "$LOG"
GW_PID=$DEVLIB_PID

HEALTHY=false
if devlib_wait_healthy "$HOST" "$PORT" 30; then
  HEALTHY=true
fi
WAIT_MS=$(( ($(date +%s%N) - START_WAIT_BEGIN) / 1000000 ))

if [[ "$HEALTHY" != "true" ]]; then
  PHASE_OK[start]=false; PHASE_MS[start]=$WAIT_MS

  # Diagnose WHY it failed.
  if [[ -n "${GW_PID:-}" ]] && ! kill -0 "$GW_PID" 2>/dev/null; then
    FAIL_REASON="start_crash"
    EXIT_CODE=$(wait "$GW_PID" 2>/dev/null; echo $?) || EXIT_CODE="unknown"
    GW_PID=""
    FAIL_DETAIL="Process exited with code $EXIT_CODE within ${WAIT_MS}ms"
  elif ss -ltnp 2>/dev/null | grep -q ":$PORT "; then
    # Port is bound but health check fails — might be a different process.
    HOLDER=$(ss -ltnp 2>/dev/null | grep ":$PORT " | head -1 || true)
    if [[ -n "${GW_PID:-}" ]] && kill -0 "$GW_PID" 2>/dev/null; then
      FAIL_REASON="start_timeout"
      FAIL_DETAIL="Process alive but /health not responding after ${WAIT_MS}ms"
    else
      FAIL_REASON="start_port_conflict"
      FAIL_DETAIL="Port $PORT held by: $HOLDER"
    fi
  else
    FAIL_REASON="start_timeout"
    FAIL_DETAIL="Process alive but /health not responding after ${WAIT_MS}ms"
  fi

  echo "FAIL ($FAIL_REASON: $FAIL_DETAIL)"

  # Show relevant logs.
  if [[ -f "$LOG" ]]; then
    echo "--- last 15 log lines ---"
    tail -15 "$LOG"
    echo "--- end ---"
  fi

  DIAG_JSON=$(python3 -c "
import json
d = {'category': '$FAIL_REASON',
     'detail': $(python3 -c "import json; print(json.dumps('''$FAIL_DETAIL'''))" 2>/dev/null || echo '""'),
     'log_file': '$LOG',
     'suggestion': ''}
if '$FAIL_REASON' == 'start_crash':
    d['suggestion'] = 'Check crash log at $LOG. Look for panic or fatal errors.'
elif '$FAIL_REASON' == 'start_port_conflict':
    d['suggestion'] = 'Kill process holding port $PORT, or use --port to change port.'
else:
    d['suggestion'] = 'Gateway did not become healthy in ${WAIT_MS}ms. Check $LOG for errors.'
print(json.dumps(d))
")

  echo "ITERATE_RESULT metric=0 build=ok server=fail checks=0/0 latency_ms=$((BUILD_MS + WAIT_MS))"
  echo "DENEB_TEST_JSON $(_emit_result_json)"
  exit 1
fi
PHASE_OK[start]=true; PHASE_MS[start]=$WAIT_MS
echo "ok (${WAIT_MS}ms)"

# --- Step 3: Run metric ---
PASS=0
TOTAL=0

_run_metric_cmd() {
  # Run a metric command or preset; sets METRIC_VAL, QUALITY_JSON, CHECK_MS, TOTAL, PASS.
  local cmd="$1"
  echo -n "metric... "
  CHECK_START=$(date +%s%N)
  METRIC_OUT=$(eval "$cmd" 2>&1) || true

  METRIC_VAL=$(echo "$METRIC_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
  if [[ -z "$METRIC_VAL" ]]; then
    echo "FAIL (no metric_value in output)"
    echo "$METRIC_OUT" | tail -5
    METRIC_VAL=0
  else
    echo "$METRIC_VAL"
  fi

  METRIC_DETAIL=$(echo "$METRIC_OUT" | grep '^DENEB_METRIC_DETAIL ' | tail -1 | sed 's/^DENEB_METRIC_DETAIL //')
  if [[ -n "$METRIC_DETAIL" ]]; then
    QUALITY_JSON=$(python3 -c "
import json
parts = '''$METRIC_DETAIL'''.split()
d = {}
for p in parts:
    if '=' in p:
        k, v = p.split('=', 1)
        try: d[k] = int(v)
        except: d[k] = v
print(json.dumps(d))
")
  fi

  CHECK_MS=$(( ($(date +%s%N) - CHECK_START) / 1000000 ))
  TOTAL=1
  PASS=$( [[ "$(echo "$METRIC_VAL > 0" | bc -l 2>/dev/null || echo 0)" == "1" ]] && echo 1 || echo 0 )
}

if [[ -n "$METRIC_PRESET" ]]; then
  # Built-in metric presets (no code generation needed).
  case "$METRIC_PRESET" in
    quality)
      _run_metric_cmd "\"$SCRIPT_DIR/quality-metric.sh\"" ;;
    combined)
      # Smoke (20%) + Quality (80%).
      echo -n "metric(combined)... "
      CHECK_START=$(date +%s%N)

      SMOKE_PASS=0
      STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null \
        | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
      [[ "$STATUS" == "ok" ]] && SMOKE_PASS=$((SMOKE_PASS+1))
      READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
      [[ "$READY" == "200" ]] && SMOKE_PASS=$((SMOKE_PASS+1))

      if [[ "$SMOKE_PASS" -eq 0 ]]; then
        METRIC_VAL=0
        echo "0 (smoke failed)"
      else
        Q_OUT=$("$SCRIPT_DIR/quality-metric.sh" 2>&1) || true
        Q_VAL=$(echo "$Q_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
        Q_VAL=${Q_VAL:-0}
        SMOKE_SCORE=$(python3 -c "print(round($SMOKE_PASS / 2 * 20))")
        Q_SCORE=$(python3 -c "print(round($Q_VAL / 100 * 80))")
        METRIC_VAL=$(python3 -c "print($SMOKE_SCORE + $Q_SCORE)")
        echo "$METRIC_VAL"
      fi

      CHECK_MS=$(( ($(date +%s%N) - CHECK_START) / 1000000 ))
      TOTAL=1
      PASS=$( [[ "$(echo "$METRIC_VAL > 0" | bc -l 2>/dev/null || echo 0)" == "1" ]] && echo 1 || echo 0 )
      ;;
    *)
      echo "Unknown metric preset: $METRIC_PRESET"
      exit 1
      ;;
  esac

elif [[ -n "$METRIC_CMD" ]]; then
  _run_metric_cmd "$METRIC_CMD"

else
  # Default: built-in smoke test (health + ready, no WebSocket).
  echo -n "smoke... "
  TOTAL=2
  CHECK_START=$(date +%s%N)

  # Collect per-check results for JSON.
  declare -a CHECK_NAMES=() CHECK_OKS=() CHECK_MSS=() CHECK_DETAILS=()

  # Run 2 HTTP checks in parallel.
  _TMP="/tmp/deneb-smoke-$$"
  C_PAR_START=$(date +%s%N)

  # Check 1: Health (background).
  (
    _s=$(date +%s%N)
    _v=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
    _ms=$(( ($(date +%s%N) - _s) / 1000000 ))
    echo "${_v}|${_ms}"
  ) > "$_TMP-h" 2>/dev/null &
  _PID_H=$!

  # Check 2: Ready (background).
  (
    _s=$(date +%s%N)
    _v=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
    _ms=$(( ($(date +%s%N) - _s) / 1000000 ))
    echo "${_v}|${_ms}"
  ) > "$_TMP-r" 2>/dev/null &
  _PID_R=$!

  wait $_PID_H $_PID_R

  # Parse health result.
  _H_RAW=$(cat "$_TMP-h" 2>/dev/null || echo "|0")
  STATUS="${_H_RAW%%|*}"; C1_MS="${_H_RAW##*|}"
  if [[ "$STATUS" == "ok" ]]; then
    PASS=$((PASS+1)); CHECK_OKS+=(true)
  else
    CHECK_OKS+=(false)
  fi
  CHECK_NAMES+=("health"); CHECK_MSS+=("$C1_MS"); CHECK_DETAILS+=("status=$STATUS")

  # Parse ready result.
  _R_RAW=$(cat "$_TMP-r" 2>/dev/null || echo "000|0")
  READY="${_R_RAW%%|*}"; C2_MS="${_R_RAW##*|}"
  if [[ "$READY" == "200" ]]; then
    PASS=$((PASS+1)); CHECK_OKS+=(true)
  else
    CHECK_OKS+=(false)
  fi
  CHECK_NAMES+=("ready"); CHECK_MSS+=("$C2_MS"); CHECK_DETAILS+=("http=$READY")

  rm -f "$_TMP-h" "$_TMP-r"
  CHECK_MS=$(( ($(date +%s%N) - C_PAR_START) / 1000000 ))
  echo "$PASS/$TOTAL (${CHECK_MS}ms)"
  METRIC_VAL=$PASS

  # Build checks JSON.
  CHECKS_JSON=$(python3 -c "
import json
names = '${CHECK_NAMES[*]}'.split()
oks = '${CHECK_OKS[*]}'.split()
mss = '${CHECK_MSS[*]}'.split()
details = '''${CHECK_DETAILS[0]}|${CHECK_DETAILS[1]}'''.split('|')
checks = []
for i in range(len(names)):
    checks.append({'name': names[i], 'ok': oks[i] == 'true', 'ms': int(mss[i]), 'detail': details[i].strip()})
print(json.dumps(checks))
")

  # Report failed checks explicitly.
  for i in "${!CHECK_NAMES[@]}"; do
    if [[ "${CHECK_OKS[$i]}" == "false" ]]; then
      echo "  FAILED: ${CHECK_NAMES[$i]} (${CHECK_DETAILS[$i]}, ${CHECK_MSS[$i]}ms)"
    fi
  done
fi

PHASE_OK[test]=$( [[ "$PASS" -eq "${TOTAL:-0}" ]] && echo true || echo false )
PHASE_MS[test]=${CHECK_MS:-0}

# --- Step 4: Check for errors in logs ---
if [[ -f "$LOG" ]]; then
  LOG_ERRORS=$(grep -cE '"level":"(error)"|panic|fatal' "$LOG" 2>/dev/null || true)
  LOG_ERRORS=${LOG_ERRORS:-0}
  if [[ "$LOG_ERRORS" -gt 0 ]]; then
    echo "  log errors: $LOG_ERRORS"
    grep -iE '"level":"error"|ERROR|panic|fatal' "$LOG" 2>/dev/null | head -5
  fi
fi

# --- Step 5: Stop ---
if [[ -n "${GW_PID:-}" ]]; then
  devlib_stop_pid "$GW_PID"
  GW_PID=""
fi

# --- Report ---
TOTAL_MS=$(( BUILD_MS + WAIT_MS + ${CHECK_MS:-0} ))

# Legacy format (backward compatible).
echo "ITERATE_RESULT metric=$METRIC_VAL build=ok server=ok checks=${PASS:-0}/${TOTAL:-0} latency_ms=$TOTAL_MS"

# Structured JSON.
echo "DENEB_TEST_JSON $(_emit_result_json)"

# --- Baseline comparison ---
if [[ "$USE_BASELINE" == "true" ]] && [[ -x "$SCRIPT_DIR/baseline.sh" ]]; then
  "$SCRIPT_DIR/baseline.sh" compare 2>&1 || true
fi

if [[ "$SAVE_BASELINE" == "true" ]] && [[ -x "$SCRIPT_DIR/baseline.sh" ]]; then
  "$SCRIPT_DIR/baseline.sh" save 2>&1 || true
fi
