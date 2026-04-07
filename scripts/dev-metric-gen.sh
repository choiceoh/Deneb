#!/usr/bin/env bash
# Metric script generator for autoresearch.
#
# Generates self-contained metric scripts that build, test, and output
# metric_value=N for use as autoresearch metric_cmd.
#
# Presets:
#   smoke     — Health + Ready + WebSocket RPC (metric_value=0~3)
#   quality   — Chat quality score (metric_value=0~100)
#   combined  — Weighted smoke + quality (metric_value=0~100)
#   custom    — Custom message quality (metric_value=0~100)
#
# Usage:
#   scripts/dev-metric-gen.sh list                         # show presets
#   scripts/dev-metric-gen.sh smoke                        # generate smoke metric
#   scripts/dev-metric-gen.sh quality                      # generate quality metric
#   scripts/dev-metric-gen.sh combined                     # generate combined metric
#   scripts/dev-metric-gen.sh custom "메시지"               # generate custom metric

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="/tmp"

# Parse arguments.
PRESET="${1:-list}"
shift || true

CUSTOM_MSG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    *) CUSTOM_MSG="$1"; shift ;;
  esac
done

_write_header() {
  local script="$1"
  cat > "$script" << 'HEADER_EOF'
#!/usr/bin/env bash
# Auto-generated metric script for autoresearch.
# Builds gateway, runs test, outputs metric_value=N.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="${AUTORESEARCH_REPO:-PLACEHOLDER_REPO}"
PORT="${METRIC_PORT:-18791}"
BINARY="/tmp/deneb-gateway-metric"
LOG="/tmp/deneb-gateway-metric.log"
HOST="127.0.0.1"

cleanup() {
  if [[ -n "${GW_PID:-}" ]]; then
    kill "$GW_PID" 2>/dev/null || true
    local w=0
    while kill -0 "$GW_PID" 2>/dev/null && (( w < 30 )); do
      sleep 0.1; w=$((w+1))
    done
    kill -9 "$GW_PID" 2>/dev/null || true
    wait "$GW_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Build.
if ! go build -C "$REPO_DIR/gateway-go" -ldflags "-s -w" -o "$BINARY" ./cmd/gateway/ 2>/tmp/deneb-metric-build.log; then
  echo "metric_value=0"
  echo "DENEB_METRIC_DETAIL build=fail"
  exit 0
fi
HEADER_EOF
  # Replace placeholder with actual repo dir.
  sed -i "s|PLACEHOLDER_REPO|$REPO_DIR|g" "$script"
}

_write_server_start() {
  local script="$1"
  cat >> "$script" << 'START_EOF'

# Start gateway.
DEV_CONFIG="/tmp/deneb-metric-config.json"
[[ -f "$DEV_CONFIG" ]] || echo '{}' > "$DEV_CONFIG"
DENEB_CONFIG_PATH="$DEV_CONFIG" "$BINARY" --bind loopback --port "$PORT" > "$LOG" 2>&1 &
GW_PID=$!

_WAIT_MS=50
for _ in $(seq 1 30); do
  curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1 && break
  if ! kill -0 "$GW_PID" 2>/dev/null; then break; fi
  sleep "$(awk "BEGIN {printf \"%.3f\", $_WAIT_MS/1000}")"
  _WAIT_MS=$(( _WAIT_MS * 2 )); (( _WAIT_MS > 300 )) && _WAIT_MS=300
done

if ! curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
  echo "metric_value=0"
  echo "DENEB_METRIC_DETAIL build=ok server=fail"
  exit 0
fi
START_EOF
}

gen_smoke() {
  local script="$OUT_DIR/deneb-metric-smoke.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'SMOKE_EOF'

# Smoke checks.
PASS=0

# 1. Health.
STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
[[ "$STATUS" == "ok" ]] && PASS=$((PASS+1))

# 2. Ready.
READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
[[ "$READY" == "200" ]] && PASS=$((PASS+1))

# 3. WebSocket RPC.
WS_OK=$(python3 -c "
import json, asyncio, websockets, time
async def main():
    try:
        ws = await asyncio.wait_for(websockets.connect('ws://$HOST:$PORT/ws', ping_interval=None), timeout=3)
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'m-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'metric','version':'1.0.0','platform':'test','mode':'control'}}}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'): print('0'); return
        rpc = {'type':'req','id':f'metric-{int(time.time()*1000)}','method':'health','params':{}}
        await ws.send(json.dumps(rpc))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        print('1' if resp.get('ok') else '0')
        await ws.close()
    except: print('0')
asyncio.run(main())
" 2>/dev/null || echo "0")
[[ "$WS_OK" == "1" ]] && PASS=$((PASS+1))

echo "metric_value=$PASS"
echo "DENEB_METRIC_DETAIL build=ok server=ok health=$([[ $STATUS == ok ]] && echo ok || echo fail) ready=$([[ $READY == 200 ]] && echo ok || echo fail) ws_rpc=$([[ $WS_OK == 1 ]] && echo ok || echo fail)"
SMOKE_EOF

  chmod +x "$script"
  echo "$script"
}

gen_quality() {
  local script="$OUT_DIR/deneb-metric-quality.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'QUALITY_EOF'

# Quality metric (delegates to dev-quality-metric.sh).
QUALITY_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" "$PORT" 2>&1) || true
METRIC_VAL=$(echo "$QUALITY_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
DETAIL=$(echo "$QUALITY_OUT" | grep '^DENEB_METRIC_DETAIL ' | tail -1)

echo "metric_value=${METRIC_VAL:-0}"
[[ -n "$DETAIL" ]] && echo "$DETAIL" || echo "DENEB_METRIC_DETAIL build=ok server=ok"
QUALITY_EOF

  chmod +x "$script"
  echo "$script"
}

gen_combined() {
  local script="$OUT_DIR/deneb-metric-combined.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'COMBINED_EOF'

# Combined metric: smoke (weight 20) + quality (weight 80).
# Smoke.
SMOKE_PASS=0
STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
[[ "$STATUS" == "ok" ]] && SMOKE_PASS=$((SMOKE_PASS+1))
READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
[[ "$READY" == "200" ]] && SMOKE_PASS=$((SMOKE_PASS+1))
WS_OK=$(python3 -c "
import json, asyncio, websockets, time
async def main():
    try:
        ws = await asyncio.wait_for(websockets.connect('ws://$HOST:$PORT/ws', ping_interval=None), timeout=3)
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'m-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'metric','version':'1.0.0','platform':'test','mode':'control'}}}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'): print('0'); return
        rpc = {'type':'req','id':f'metric-{int(time.time()*1000)}','method':'health','params':{}}
        await ws.send(json.dumps(rpc))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        print('1' if resp.get('ok') else '0')
        await ws.close()
    except: print('0')
asyncio.run(main())
" 2>/dev/null || echo "0")
[[ "$WS_OK" == "1" ]] && SMOKE_PASS=$((SMOKE_PASS+1))

# If smoke fails completely, return 0.
if [[ "$SMOKE_PASS" -eq 0 ]]; then
  echo "metric_value=0"
  echo "DENEB_METRIC_DETAIL build=ok smoke=0/3 quality=skip"
  exit 0
fi

# Quality.
QUALITY_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" "$PORT" 2>&1) || true
QUALITY_VAL=$(echo "$QUALITY_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
QUALITY_VAL=${QUALITY_VAL:-0}

# Combined: smoke_score(0~20) + quality_score(0~80).
SMOKE_SCORE=$(python3 -c "print(round($SMOKE_PASS / 3 * 20))")
QUALITY_SCORE=$(python3 -c "print(round($QUALITY_VAL / 100 * 80))")
TOTAL=$(python3 -c "print($SMOKE_SCORE + $QUALITY_SCORE)")

echo "metric_value=$TOTAL"
echo "DENEB_METRIC_DETAIL build=ok smoke=$SMOKE_PASS/3($SMOKE_SCORE) quality=$QUALITY_VAL($QUALITY_SCORE) combined=$TOTAL"
COMBINED_EOF

  chmod +x "$script"
  echo "$script"
}

gen_custom() {
  local message="$1"
  local script="$OUT_DIR/deneb-metric-custom.sh"
  _write_header "$script"
  _write_server_start "$script"

  # Escape message for embedding in script.
  local escaped_msg
  escaped_msg=$(python3 -c "import json; print(json.dumps('$message'))")

  cat >> "$script" << CUSTOM_EOF

# Custom message quality metric.
QUALITY_OUT=\$("\$REPO_DIR/scripts/dev-quality-metric.sh" "\$PORT" $escaped_msg 2>&1) || true
METRIC_VAL=\$(echo "\$QUALITY_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
DETAIL=\$(echo "\$QUALITY_OUT" | grep '^DENEB_METRIC_DETAIL ' | tail -1)

echo "metric_value=\${METRIC_VAL:-0}"
[[ -n "\$DETAIL" ]] && echo "\$DETAIL" || echo "DENEB_METRIC_DETAIL build=ok server=ok"
CUSTOM_EOF

  chmod +x "$script"
  echo "$script"
}

cmd_list() {
  echo "Available metric presets:"
  echo ""
  echo "  smoke      Health + Ready + WebSocket RPC (metric_value=0~3, ~2s)"
  echo "  quality    Chat quality score (metric_value=0~100, ~30s)"
  echo "  combined   Smoke(20%) + Quality(80%) (metric_value=0~100, ~30s)"
  echo "  custom     Custom message quality (metric_value=0~100, ~30s)"
  echo "             Usage: dev-metric-gen.sh custom \"메시지\""
  echo ""
  echo "Generated scripts go to /tmp/deneb-metric-*.sh"
  echo "Use as: autoresearch init --metric_cmd /tmp/deneb-metric-*.sh"
}

# --- Main ---
case "$PRESET" in
  list)     cmd_list ;;
  smoke)    gen_smoke ;;
  quality)  gen_quality ;;
  combined) gen_combined ;;
  custom)
    if [[ -z "$CUSTOM_MSG" ]]; then
      echo "Usage: dev-metric-gen.sh custom \"메시지\""
      exit 1
    fi
    gen_custom "$CUSTOM_MSG"
    ;;
  *)
    echo "Unknown preset: $PRESET"
    cmd_list
    exit 1
    ;;
esac
