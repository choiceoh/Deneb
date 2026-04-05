#!/usr/bin/env bash
# Direct iteration loop for Claude Code.
#
# Claude Code runs this to test a code change against the live gateway.
# One-shot: build → start → metric → stop → report.
# Claude Code calls this repeatedly, modifying code between iterations.
#
# Usage:
#   scripts/dev-iterate.sh                    # default: smoke test (3 checks)
#   scripts/dev-iterate.sh --metric CMD       # custom metric command
#   scripts/dev-iterate.sh --port 18791       # custom port (default: 18791)
#   scripts/dev-iterate.sh --vchat            # test through Telegram pipeline
#   scripts/dev-iterate.sh --vchat --scenario korean  # specific vchat scenario
#   scripts/dev-iterate.sh --baseline         # compare against saved baseline
#   scripts/dev-iterate.sh --save-baseline    # save result as new baseline
#
# Exit code: 0 if metric improved or stable, 1 if degraded or build failed.
#
# Output format (last two lines, machine-readable):
#   ITERATE_RESULT metric=3 build=ok server=ok checks=3/3 latency_ms=147
#   DENEB_TEST_JSON {"version":1,...}

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

PORT="${ITERATE_PORT:-18791}"
BINARY="/tmp/deneb-gateway-iterate"
LOG="/tmp/deneb-gateway-iterate.log"
HOST="127.0.0.1"
RESULT_FILE="/tmp/deneb-iterate-result.json"
BUILD_LOG="/tmp/deneb-iterate-build.log"
LOCK_FILE="/tmp/deneb-iterate.lock"

# Parse arguments.
METRIC_CMD=""
USE_VCHAT=false
VCHAT_SCENARIO="all"
USE_BASELINE=false
SAVE_BASELINE=false
PROD_PARITY="${DEV_PROD_PARITY:-false}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --metric) METRIC_CMD="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --vchat) USE_VCHAT=true; shift ;;
    --scenario) VCHAT_SCENARIO="$2"; shift 2 ;;
    --baseline) USE_BASELINE=true; shift ;;
    --save-baseline) SAVE_BASELINE=true; shift ;;
    --prod-parity) PROD_PARITY=true; shift ;;
    *) shift ;;
  esac
done

# --- Concurrent execution guard ---
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
  echo "BLOCKED: another dev-iterate.sh is running (lock: $LOCK_FILE)"
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
    'prod_parity': tobool('$PROD_PARITY'),
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

# --- Robust process management ---

_verify_port_free() {
  local port="$1" retries=0 wait_ms=30
  while (( retries < 15 )); do
    if ! ss -ltnp 2>/dev/null | grep -q ":$port "; then
      return 0
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $wait_ms/1000}")"
    retries=$((retries+1))
    wait_ms=$(( wait_ms * 2 )); (( wait_ms > 200 )) && wait_ms=200
  done
  local holder
  holder=$(ss -ltnp 2>/dev/null | grep ":$port " | head -1 || true)
  echo "  WARN: port $port still held: $holder" >&2
  return 1
}

cleanup() {
  if [[ -n "${GW_PID:-}" ]]; then
    # SIGTERM first.
    kill "$GW_PID" 2>/dev/null || true
    # Wait up to 3s for graceful shutdown.
    local waited=0
    while kill -0 "$GW_PID" 2>/dev/null && (( waited < 30 )); do
      sleep 0.1; waited=$((waited+1))
    done
    # SIGKILL fallback if still alive.
    if kill -0 "$GW_PID" 2>/dev/null; then
      kill -9 "$GW_PID" 2>/dev/null || true
      wait "$GW_PID" 2>/dev/null || true
    fi
    GW_PID=""
  fi
  # Stop vchat if we started it.
  if [[ "${VCHAT_STARTED:-}" == "true" ]]; then
    python3 "$SCRIPT_DIR/vchat.py" stop 2>/dev/null || true
    VCHAT_STARTED=""
  fi
  _verify_port_free "$PORT" || true
}
trap cleanup EXIT

# --- Step 1: Build ---
echo -n "build... "
BUILD_START=$(date +%s%N)
DENEB_VERSION=$(git -C "$REPO_DIR" tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')
if ! go build -C "$REPO_DIR/gateway-go" -ldflags "-s -w -X main.Version=${DENEB_VERSION:-dev}" -o "$BINARY" ./cmd/gateway/ 2>"$BUILD_LOG"; then
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
if [[ "$USE_VCHAT" == "true" ]]; then
  # Start through vchat (mock Telegram + gateway with Telegram config).
  echo -n "vchat-start... "
  START_WAIT_BEGIN=$(date +%s%N)

  VCHAT_MOCK_PORT=$((PORT + 1))
  export VCHAT_MOCK_PORT
  export VCHAT_GATEWAY_PORT="$PORT"
  export VCHAT_BINARY="$BINARY"

  python3 "$SCRIPT_DIR/vchat.py" start --no-build --background 2>/dev/null &
  VCHAT_STARTER_PID=$!
  VCHAT_STARTED=true

  # Wait for mock + gateway to be ready (exponential backoff).
  HEALTHY=false
  _WAIT_MS=50
  for _ in $(seq 1 50); do
    if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1 && \
       curl -sf "http://$HOST:$VCHAT_MOCK_PORT/control/status" > /dev/null 2>&1; then
      HEALTHY=true
      break
    fi
    # Check if starter crashed.
    if ! kill -0 "$VCHAT_STARTER_PID" 2>/dev/null; then
      break
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $_WAIT_MS/1000}")"
    _WAIT_MS=$(( _WAIT_MS * 2 )); (( _WAIT_MS > 300 )) && _WAIT_MS=300
  done
  WAIT_MS=$(( ($(date +%s%N) - START_WAIT_BEGIN) / 1000000 ))
else
  # Start raw gateway; prod-parity uses real config (minus Telegram), default uses {}.
  echo -n "start... "
  DEV_CONFIG="/tmp/deneb-iterate-config.json"
  if [[ "$PROD_PARITY" == "true" ]]; then
    "$SCRIPT_DIR/dev-config-gen.sh" --out "$DEV_CONFIG" >/dev/null 2>&1
  elif [[ ! -f "$DEV_CONFIG" ]]; then
    echo '{}' > "$DEV_CONFIG"
  fi
  DENEB_CONFIG_PATH="$DEV_CONFIG" "$BINARY" --bind loopback --port "$PORT" > "$LOG" 2>&1 &
  GW_PID=$!

  # Wait for health (exponential backoff: 50ms → 300ms cap).
  START_WAIT_BEGIN=$(date +%s%N)
  HEALTHY=false
  _WAIT_MS=50
  for _ in $(seq 1 30); do
    if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
      HEALTHY=true
      break
    fi
    # Early exit if process crashed.
    if ! kill -0 "$GW_PID" 2>/dev/null; then
      break
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $_WAIT_MS/1000}")"
    _WAIT_MS=$(( _WAIT_MS * 2 )); (( _WAIT_MS > 300 )) && _WAIT_MS=300
  done
  WAIT_MS=$(( ($(date +%s%N) - START_WAIT_BEGIN) / 1000000 ))
fi

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

if [[ "$USE_VCHAT" == "true" ]]; then
  # vchat quality test through Telegram pipeline.
  echo -n "vchat-quality ($VCHAT_SCENARIO)... "
  CHECK_START=$(date +%s%N)

  VCHAT_OUT=$(python3 "$SCRIPT_DIR/dev-vchat-quality.py" \
    --port "$VCHAT_MOCK_PORT" --gateway-port "$PORT" \
    --scenario "$VCHAT_SCENARIO" --json 2>&1) || true

  # Parse JSON result.
  VCHAT_JSON=$(echo "$VCHAT_OUT" | grep '^VCHAT_QUALITY_JSON ' | tail -1 | sed 's/^VCHAT_QUALITY_JSON //')
  if [[ -n "$VCHAT_JSON" ]]; then
    PASS=$(echo "$VCHAT_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('passed_checks',0))")
    TOTAL=$(echo "$VCHAT_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('total_checks',0))")
    CHECKS_JSON=$(echo "$VCHAT_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(json.dumps(d.get('checks',[])))")
    QUALITY_JSON=$(echo "$VCHAT_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin); print(json.dumps(d.get('quality',{})))")
    METRIC_VAL=$PASS
  else
    # Fallback: parse human-readable output.
    echo "  (no JSON output from vchat-quality)"
    echo "$VCHAT_OUT" | tail -5
    METRIC_VAL=0
  fi

  CHECK_MS=$(( ($(date +%s%N) - CHECK_START) / 1000000 ))
  echo "$PASS/$TOTAL (${CHECK_MS}ms)"

elif [[ -n "$METRIC_CMD" ]]; then
  # Custom metric command.
  echo -n "metric... "
  CHECK_START=$(date +%s%N)
  METRIC_OUT=$(eval "$METRIC_CMD" 2>&1) || true

  # Extract metric_value=N from output.
  METRIC_VAL=$(echo "$METRIC_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
  if [[ -z "$METRIC_VAL" ]]; then
    echo "FAIL (no metric_value in output)"
    echo "$METRIC_OUT" | tail -5
    METRIC_VAL=0
  else
    echo "$METRIC_VAL"
  fi

  # Extract quality breakdown if present (from dev-quality-metric.sh).
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

else
  # Default: built-in smoke test.
  echo -n "smoke... "
  TOTAL=3
  CHECK_START=$(date +%s%N)

  # Collect per-check results for JSON.
  declare -a CHECK_NAMES=() CHECK_OKS=() CHECK_MSS=() CHECK_DETAILS=()

  # Run all 3 checks in parallel (health, ready, ws_rpc).
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

  # Check 3: WebSocket handshake + RPC (background).
  (
    _s=$(date +%s%N)
    _v=$(python3 -c "
import json, asyncio, time, websockets
async def main():
    try:
        ws = await asyncio.wait_for(websockets.connect('ws://$HOST:$PORT/ws', ping_interval=None), timeout=3)
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'it-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'iterate','version':'1.0.0','platform':'test','mode':'control'}}}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'): print('0|handshake_rejected'); return
        rpc = {'type':'req','id':f'it-{int(time.time()*1000)}','method':'health','params':{}}
        await ws.send(json.dumps(rpc))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        print('1|ok' if resp.get('ok') else '0|rpc_failed')
        await ws.close()
    except asyncio.TimeoutError:
        print('0|timeout')
    except ConnectionRefusedError:
        print('0|connection_refused')
    except Exception as e:
        print(f'0|{type(e).__name__}')
asyncio.run(main())
" 2>/dev/null || echo "0|python_error")
    _ms=$(( ($(date +%s%N) - _s) / 1000000 ))
    echo "${_v}|${_ms}"
  ) > "$_TMP-w" 2>/dev/null &
  _PID_W=$!

  wait $_PID_H $_PID_R $_PID_W

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

  # Parse ws_rpc result.
  _W_RAW=$(cat "$_TMP-w" 2>/dev/null || echo "0|python_error|0")
  _W_BODY="${_W_RAW%|*}"; C3_MS="${_W_RAW##*|}"
  WS_OK="${_W_BODY%%|*}"; WS_DETAIL="${_W_BODY#*|}"
  if [[ "$WS_OK" == "1" ]]; then
    PASS=$((PASS+1)); CHECK_OKS+=(true)
  else
    CHECK_OKS+=(false)
  fi
  CHECK_NAMES+=("ws_rpc"); CHECK_MSS+=("$C3_MS"); CHECK_DETAILS+=("$WS_DETAIL")

  rm -f "$_TMP-h" "$_TMP-r" "$_TMP-w"
  CHECK_MS=$(( ($(date +%s%N) - C_PAR_START) / 1000000 ))
  echo "$PASS/$TOTAL (${CHECK_MS}ms)"
  METRIC_VAL=$PASS

  # Build checks JSON.
  CHECKS_JSON=$(python3 -c "
import json
names = '${CHECK_NAMES[*]}'.split()
oks = '${CHECK_OKS[*]}'.split()
mss = '${CHECK_MSS[*]}'.split()
details = '''${CHECK_DETAILS[0]}|${CHECK_DETAILS[1]}|${CHECK_DETAILS[2]}'''.split('|')
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
if [[ "$USE_VCHAT" == "true" ]]; then
  python3 "$SCRIPT_DIR/vchat.py" stop 2>/dev/null || true
  VCHAT_STARTED=""
else
  if [[ -n "${GW_PID:-}" ]]; then
    kill "$GW_PID" 2>/dev/null || true
    local_wait=0
    while kill -0 "$GW_PID" 2>/dev/null && (( local_wait < 30 )); do
      sleep 0.1; local_wait=$((local_wait+1))
    done
    if kill -0 "$GW_PID" 2>/dev/null; then
      kill -9 "$GW_PID" 2>/dev/null || true
      wait "$GW_PID" 2>/dev/null || true
    fi
    GW_PID=""
  fi
fi

# --- Report ---
TOTAL_MS=$(( BUILD_MS + WAIT_MS + ${CHECK_MS:-0} ))

# Legacy format (backward compatible).
echo "ITERATE_RESULT metric=$METRIC_VAL build=ok server=ok checks=${PASS:-0}/${TOTAL:-0} latency_ms=$TOTAL_MS"

# Structured JSON.
echo "DENEB_TEST_JSON $(_emit_result_json)"

# --- Baseline comparison ---
if [[ "$USE_BASELINE" == "true" ]] && [[ -x "$SCRIPT_DIR/dev-baseline.sh" ]]; then
  "$SCRIPT_DIR/dev-baseline.sh" compare 2>&1 || true
fi

if [[ "$SAVE_BASELINE" == "true" ]] && [[ -x "$SCRIPT_DIR/dev-baseline.sh" ]]; then
  "$SCRIPT_DIR/dev-baseline.sh" save 2>&1 || true
fi
