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
#
# Exit code: 0 if metric improved or stable, 1 if degraded or build failed.
#
# Output format (last line, machine-readable):
#   ITERATE_RESULT metric=3 build=ok server=ok checks=3/3 latency_ms=147

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

PORT="${ITERATE_PORT:-18791}"
BINARY="/tmp/deneb-gateway-iterate"
LOG="/tmp/deneb-gateway-iterate.log"
HOST="127.0.0.1"

# Parse arguments.
METRIC_CMD=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --metric) METRIC_CMD="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    *) shift ;;
  esac
done

cleanup() {
  # Kill gateway if we started it.
  if [[ -n "${GW_PID:-}" ]]; then
    kill "$GW_PID" 2>/dev/null || true
    wait "$GW_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# --- Step 1: Build ---
echo -n "build... "
BUILD_START=$(date +%s%N)
if ! go build -C "$REPO_DIR/gateway-go" -ldflags "-s -w" -o "$BINARY" ./cmd/gateway/ 2>/tmp/deneb-iterate-build.log; then
  echo "FAIL"
  tail -5 /tmp/deneb-iterate-build.log
  echo "ITERATE_RESULT metric=0 build=fail server=skip checks=0/0 latency_ms=0"
  exit 1
fi
BUILD_MS=$(( ($(date +%s%N) - BUILD_START) / 1000000 ))
echo "ok (${BUILD_MS}ms)"

# --- Step 2: Start gateway (no Telegram — avoids 409 conflict with production) ---
echo -n "start... "
DEV_CONFIG="/tmp/deneb-iterate-config.json"
if [[ ! -f "$DEV_CONFIG" ]]; then
  echo '{}' > "$DEV_CONFIG"
fi
DENEB_CONFIG_PATH="$DEV_CONFIG" "$BINARY" --bind loopback --port "$PORT" > "$LOG" 2>&1 &
GW_PID=$!

# Wait for health.
START_WAIT=$(date +%s%N)
HEALTHY=false
for _ in $(seq 1 40); do
  if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  sleep 0.15
done
WAIT_MS=$(( ($(date +%s%N) - START_WAIT) / 1000000 ))

if [[ "$HEALTHY" != "true" ]]; then
  echo "FAIL (not healthy after ${WAIT_MS}ms)"
  tail -10 "$LOG"
  echo "ITERATE_RESULT metric=0 build=ok server=fail checks=0/0 latency_ms=$WAIT_MS"
  exit 1
fi
echo "ok (${WAIT_MS}ms)"

# --- Step 3: Run metric ---
if [[ -n "$METRIC_CMD" ]]; then
  # Custom metric command.
  echo -n "metric... "
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
else
  # Default: built-in smoke test.
  echo -n "smoke... "
  PASS=0
  TOTAL=3
  CHECK_START=$(date +%s%N)

  # Check 1: Health.
  STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
  [[ "$STATUS" == "ok" ]] && PASS=$((PASS+1))

  # Check 2: Ready.
  READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
  [[ "$READY" == "200" ]] && PASS=$((PASS+1))

  # Check 3: WebSocket handshake + RPC.
  WS_OK=$(python3 -c "
import json, asyncio, time, websockets
async def main():
    try:
        ws = await asyncio.wait_for(websockets.connect('ws://$HOST:$PORT/ws'), timeout=3)
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'it-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'iterate','version':'1.0.0','platform':'test','mode':'control'}}}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'): print('0'); return
        rpc = {'type':'req','id':f'it-{int(time.time()*1000)}','method':'health','params':{}}
        await ws.send(json.dumps(rpc))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        print('1' if resp.get('ok') else '0')
        await ws.close()
    except: print('0')
asyncio.run(main())
" 2>/dev/null || echo "0")
  [[ "$WS_OK" == "1" ]] && PASS=$((PASS+1))

  CHECK_MS=$(( ($(date +%s%N) - CHECK_START) / 1000000 ))
  echo "$PASS/$TOTAL (${CHECK_MS}ms)"
  METRIC_VAL=$PASS
fi

# --- Step 4: Check for errors in logs ---
ERROR_COUNT=$(grep -cE '"level":"(error)"|panic|fatal' "$LOG" 2>/dev/null || true)
ERROR_COUNT=${ERROR_COUNT:-0}
if [[ "$ERROR_COUNT" -gt 0 ]]; then
  echo "  warnings: $ERROR_COUNT error(s) in log"
  grep -iE '"level":"error"|ERROR|panic|fatal' "$LOG" | head -3
fi

# --- Step 5: Stop ---
kill "$GW_PID" 2>/dev/null || true
wait "$GW_PID" 2>/dev/null || true
GW_PID=""

# --- Report ---
TOTAL_MS=$(( BUILD_MS + WAIT_MS + ${CHECK_MS:-0} ))
echo "ITERATE_RESULT metric=$METRIC_VAL build=ok server=ok checks=${PASS:-0}/${TOTAL:-0} latency_ms=$TOTAL_MS"
