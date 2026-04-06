#!/usr/bin/env bash
# Live test helper for Claude Code development workflow.
#
# Usage:
#   scripts/dev-live-test.sh build              Build gateway from current tree
#   scripts/dev-live-test.sh start              Start dev gateway on port 18790
#   scripts/dev-live-test.sh stop               Stop dev gateway
#   scripts/dev-live-test.sh restart            Rebuild + restart
#   scripts/dev-live-test.sh status             Check if dev gateway is running
#   scripts/dev-live-test.sh health             Hit /health endpoint
#   scripts/dev-live-test.sh smoke              Run full smoke test (health + WS handshake + RPC)
#   scripts/dev-live-test.sh rpc METHOD [P]     Send a single RPC call via WebSocket
#   scripts/dev-live-test.sh session CMDS...    Multi-turn WebSocket session (multiple RPCs on one connection)
#   scripts/dev-live-test.sh chat MESSAGE       Send a chat message and stream the full response
#   scripts/dev-live-test.sh quality [SCENARIO]  Run quality tests (300 cases, YAML-driven)
#   scripts/dev-live-test.sh quality-custom MSG Run quality test with custom message
#   scripts/dev-live-test.sh quality-list       List all available quality tests
#   scripts/dev-live-test.sh quality-history    Show past quality test runs
#   scripts/dev-live-test.sh quality-compare A B Compare two runs
#   scripts/dev-live-test.sh quality-trend NAME  Score trend for a test
#   scripts/dev-live-test.sh logs [N]           Tail dev gateway logs (default: 50 lines)
#   scripts/dev-live-test.sh logs-watch         Follow dev gateway logs in real-time (like tail -f)
#   scripts/dev-live-test.sh logs-grep PATTERN  Search logs for pattern
#   scripts/dev-live-test.sh logs-errors        Show only error/warning lines from logs
#   scripts/dev-live-test.sh logs-since SECS    Show logs from last N seconds
#
# Reproduction (AI agent reproduces user-reported symptoms):
#   scripts/dev-live-test.sh chat-check MSG [--expect PAT] [--expect-tool TOOL] ...
#   scripts/dev-live-test.sh multi-chat MSG1 MSG2 MSG3 [--expect-context PAT]
#   scripts/dev-live-test.sh tool-check TOOL_NAME MSG
#
# Real Telegram E2E (Telethon — actual Telegram servers, not mock):
#   scripts/dev-live-test.sh e2e-quality [SCENARIO]    Real e2e quality test
#   scripts/dev-live-test.sh e2e-quality-custom MSG    Custom message e2e test
#
# The dev instance runs on port 18790 (separate from production on 18789).
#
# Config: always uses production config with dev bot token (via dev-config-gen.sh).
# This exercises the same code paths as production (providers, auth, hooks,
# agents, sessions, logging) with a separate Telegram bot to avoid 409 conflicts.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

DEV_PORT="${DEV_LIVE_PORT:-18790}"
DEV_BINARY="/tmp/deneb-gateway-live"
DEV_PID_FILE="/tmp/deneb-gateway-live.pid"
DEV_LOG="/tmp/deneb-gateway-live.log"
DEV_HOST="127.0.0.1"

# Version from git tags.
DENEB_VERSION=$(git -C "$REPO_DIR" tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')

cmd_build() {
  echo "==> Building gateway from $(basename "$REPO_DIR")..."
  cd "$REPO_DIR"
  go build -C gateway-go -ldflags "-s -w -X main.Version=${DENEB_VERSION:-dev}" -o "$DEV_BINARY" ./cmd/gateway/
  echo "    Binary: $DEV_BINARY ($(du -h "$DEV_BINARY" | cut -f1))"
}

cmd_start() {
  if _is_running; then
    echo "Dev gateway already running (PID $(cat "$DEV_PID_FILE")) on port $DEV_PORT"
    return 0
  fi

  if [[ ! -x "$DEV_BINARY" ]]; then
    echo "No binary found, building first..."
    cmd_build
  fi

  # Config: generate dev config from production (with dev bot token).
  local dev_config="/tmp/deneb-dev-config.json"
  "$SCRIPT_DIR/dev-config-gen.sh" --out "$dev_config" >/dev/null 2>&1
  if [[ -n "${DENEB_DEV_TELEGRAM_TOKEN:-}" ]]; then
    echo "    Config: production (Telegram: dev bot active)"
  else
    echo "    Config: production (Telegram: disabled, set DENEB_DEV_TELEGRAM_TOKEN)"
  fi

  echo "==> Starting dev gateway on $DEV_HOST:$DEV_PORT..."
  DENEB_CONFIG_PATH="$dev_config" nohup "$DEV_BINARY" --bind loopback --port "$DEV_PORT" > "$DEV_LOG" 2>&1 &
  local pid=$!
  echo "$pid" > "$DEV_PID_FILE"

  # Wait for health (exponential backoff: 50ms → 300ms cap).
  local retries=0 wait_ms=50
  while (( retries < 25 )); do
    if curl -sf "http://$DEV_HOST:$DEV_PORT/health" > /dev/null 2>&1; then
      echo "    Running (PID $pid, port $DEV_PORT)"
      return 0
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $wait_ms/1000}")"
    wait_ms=$(( wait_ms * 2 )); (( wait_ms > 300 )) && wait_ms=300
    retries=$((retries + 1))
  done

  echo "    WARN: Gateway started but /health not responding after 6s"
  echo "    Check logs: scripts/dev-live-test.sh logs"
  return 1
}

cmd_stop() {
  if ! _is_running; then
    echo "Dev gateway not running"
    return 0
  fi

  local pid
  pid=$(cat "$DEV_PID_FILE")
  echo "==> Stopping dev gateway (PID $pid)..."
  kill "$pid" 2>/dev/null || true
  rm -f "$DEV_PID_FILE"

  # Wait for port release (exponential backoff).
  local retries=0 wait_ms=30
  while (( retries < 15 )); do
    if ! ss -ltnp 2>/dev/null | grep -q ":$DEV_PORT "; then
      echo "    Stopped"
      return 0
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $wait_ms/1000}")"
    wait_ms=$(( wait_ms * 2 )); (( wait_ms > 200 )) && wait_ms=200
    retries=$((retries + 1))
  done
  echo "    WARN: Port $DEV_PORT still in use"
}

cmd_restart() {
  cmd_stop
  cmd_build
  cmd_start
}

cmd_status() {
  if _is_running; then
    local pid
    pid=$(cat "$DEV_PID_FILE")
    echo "Dev gateway: RUNNING (PID $pid, port $DEV_PORT)"
    curl -sf "http://$DEV_HOST:$DEV_PORT/health" 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "(health endpoint not responding)"
  else
    echo "Dev gateway: STOPPED"
  fi
}

cmd_health() {
  curl -sf "http://$DEV_HOST:$DEV_PORT/health" | python3 -m json.tool
}

cmd_smoke() {
  echo "==> Smoke test against $DEV_HOST:$DEV_PORT (parallel)"

  # Run all 3 checks in parallel.
  local _tmp="/tmp/deneb-livetest-smoke-$$"

  (curl -sf "http://$DEV_HOST:$DEV_PORT/health" 2>/dev/null \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null \
    || echo "") > "$_tmp-h" &
  local pid_h=$!

  (curl -sf -o /dev/null -w "%{http_code}" "http://$DEV_HOST:$DEV_PORT/ready" 2>/dev/null \
    || echo "000") > "$_tmp-r" &
  local pid_r=$!

  local ws_skip=false
  if command -v python3 &>/dev/null; then
    (_ws_rpc "health" "{}" > /dev/null 2>&1 && echo "ok" || echo "fail") > "$_tmp-w" &
    local pid_w=$!
  else
    echo "skip" > "$_tmp-w"
    ws_skip=true
  fi

  wait $pid_h $pid_r ${pid_w:-}

  # Evaluate results.
  local failed=0

  local status
  status=$(cat "$_tmp-h" 2>/dev/null || echo "")
  echo -n "  [1/3] GET /health ... "
  if [[ "$status" == "ok" ]]; then echo "OK"; else echo "FAIL (status=$status)"; failed=1; fi

  local ready_code
  ready_code=$(cat "$_tmp-r" 2>/dev/null || echo "000")
  echo -n "  [2/3] GET /ready ... "
  if [[ "$ready_code" == "200" ]]; then echo "OK"; else echo "FAIL (HTTP $ready_code)"; failed=1; fi

  local ws_result
  ws_result=$(cat "$_tmp-w" 2>/dev/null || echo "fail")
  echo -n "  [3/3] WebSocket RPC (health) ... "
  if [[ "$ws_skip" == "true" ]]; then echo "SKIP (python3 not available)"
  elif [[ "$ws_result" == "ok" ]]; then echo "OK"
  else echo "FAIL"; failed=1; fi

  rm -f "$_tmp-h" "$_tmp-r" "$_tmp-w"

  if (( failed )); then return 1; fi
  echo "==> All smoke tests passed"

  # Brief parity note.
  if [[ -n "${DENEB_DEV_TELEGRAM_TOKEN:-}" ]]; then
    echo "    (config: production, Telegram: dev bot)"
  else
    echo "    (config: production, Telegram: disabled)"
  fi
}

cmd_parity() {
  echo "==> Dev vs Production Parity Report"
  echo ""

  local issues=0

  # 1. Config.
  local prod_config="${HOME}/.deneb/deneb.json"
  echo "--- Config ---"
  if [[ ! -f "$prod_config" ]]; then
    echo "  [GAP]  No production config at $prod_config"
    echo "         Dev config will be empty — providers/agents/hooks not loaded"
    issues=$((issues + 1))
  else
    echo "  [OK]   Production config: $prod_config ($(wc -c < "$prod_config") bytes)"
  fi
  echo ""

  # 2. Rust build features.
  echo "--- Rust FFI & Features ---"
  if _is_running; then
    local health_json
    health_json=$(curl -sf "http://$DEV_HOST:$DEV_PORT/health" 2>/dev/null || echo "{}")
    local ffi_status
    ffi_status=$(echo "$health_json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('rustFfi','unknown'))" 2>/dev/null || echo "unknown")
    local vega_status
    vega_status=$(echo "$health_json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('vegaEnabled','unknown'))" 2>/dev/null || echo "unknown")

    if [[ "$ffi_status" == "True" || "$ffi_status" == "true" ]]; then
      echo "  [OK]   Rust FFI: available"
    else
      echo "  [GAP]  Rust FFI: not available (production requires FFI)"
      echo "         Fix: make rust && rebuild"
      issues=$((issues + 1))
    fi
    if [[ "$vega_status" == "True" || "$vega_status" == "true" ]]; then
      echo "  [OK]   Vega search: enabled"
    else
      echo "  [GAP]  Vega search: disabled (production has Vega+ML+CUDA)"
      echo "         Fix: make rust-dgx && rebuild"
      issues=$((issues + 1))
    fi
  else
    # Check static lib feature from binary.
    local lib_path="$REPO_DIR/core-rs/target/release/libdeneb_core.a"
    if [[ -f "$lib_path" ]]; then
      echo "  [OK]   Rust static lib exists ($(du -h "$lib_path" | cut -f1))"
      # Larger lib likely has more features (core ~5MB, vega ~15MB, dgx ~30MB+).
      local lib_kb
      lib_kb=$(du -k "$lib_path" | cut -f1)
      if (( lib_kb < 10000 )); then
        echo "  [GAP]  Static lib is small (${lib_kb}KB) — likely core-only, no Vega/ML"
        echo "         Production uses: make rust-dgx (Vega+ML+CUDA)"
        issues=$((issues + 1))
      fi
    else
      echo "  [GAP]  No Rust static lib found at $lib_path"
      echo "         Fix: make rust (or make rust-dgx for full parity)"
      issues=$((issues + 1))
    fi
  fi
  echo ""

  # 3. Telegram parity.
  echo "--- Telegram ---"
  if [[ -n "${DENEB_DEV_TELEGRAM_TOKEN:-}" ]]; then
    echo "  [OK]   DENEB_DEV_TELEGRAM_TOKEN: set (dev bot for port $DEV_PORT)"
  else
    echo "  [GAP]  DENEB_DEV_TELEGRAM_TOKEN: not set (Telegram pipeline disabled in dev)"
    echo "         Fix: create a test bot via @BotFather and set DENEB_DEV_TELEGRAM_TOKEN in ~/.deneb/.env"
    issues=$((issues + 1))
  fi
  if [[ -n "${DENEB_ITERATE_TELEGRAM_TOKEN:-}" ]]; then
    echo "  [OK]   DENEB_ITERATE_TELEGRAM_TOKEN: set (iterate bot for port 18791)"
  else
    echo "  [INFO] DENEB_ITERATE_TELEGRAM_TOKEN: not set (iterate will share dev token or disable)"
  fi
  echo ""

  # 4. Environment variables.
  echo "--- Key Environment Variables ---"
  local env_vars=("GEMINI_API_KEY" "DENEB_EMBED_MODEL" "GITHUB_WEBHOOK_SECRET")
  for var in "${env_vars[@]}"; do
    if [[ -n "${!var:-}" ]]; then
      echo "  [OK]   $var: set"
    else
      echo "  [INFO] $var: not set (loaded at runtime from .env if present)"
    fi
  done
  # Check if .env files exist.
  if [[ -f "$HOME/.deneb/.env" ]]; then
    echo "  [OK]   ~/.deneb/.env: exists (loaded by gateway at startup)"
  else
    echo "  [INFO] ~/.deneb/.env: not found"
  fi
  echo ""

  # 5. Port/binding.
  echo "--- Network ---"
  echo "  [INFO] Dev port: $DEV_PORT (production: 18789)"
  echo "  [INFO] Dev bind: loopback (production: config-driven)"
  echo ""

  # 6. Summary.
  if (( issues == 0 )); then
    echo "==> No parity gaps detected"
  else
    echo "==> $issues parity gap(s) found"
  fi
}

cmd_rpc() {
  local method="${1:-}"
  local params="${2:-{}}"
  if [[ -z "$method" ]]; then
    echo "Usage: scripts/dev-live-test.sh rpc METHOD [PARAMS_JSON]"
    return 1
  fi
  _ws_rpc "$method" "$params"
}

# Multi-turn WebSocket session: send multiple RPCs on the same connection.
# Usage: scripts/dev-live-test.sh session "health" "session.list {}" "channel.list {}"
# Each argument is "METHOD [PARAMS_JSON]". Maintains one WebSocket connection throughout.
cmd_session() {
  if [[ $# -eq 0 ]]; then
    echo "Usage: scripts/dev-live-test.sh session CMD1 [CMD2 CMD3 ...]"
    echo "  Each CMD is: METHOD [JSON_PARAMS]"
    echo "  Example: scripts/dev-live-test.sh session 'health' 'session.list {}' 'channel.list {}'"
    return 1
  fi

  # Build JSON array of commands.
  local cmds_json="["
  local first=true
  for cmd in "$@"; do
    local method params
    method=$(echo "$cmd" | awk '{print $1}')
    params=$(echo "$cmd" | awk '{$1=""; print $0}' | sed 's/^ *//')
    [[ -z "$params" ]] && params="{}"
    if [[ "$first" == "true" ]]; then
      first=false
    else
      cmds_json+=","
    fi
    cmds_json+="{\"method\":\"$method\",\"params\":$params}"
  done
  cmds_json+="]"

  python3 -c "
import json, asyncio, sys, time, websockets

CMDS = json.loads('$cmds_json')

async def main():
    uri = 'ws://$DEV_HOST:$DEV_PORT/ws'
    async with websockets.connect(uri, max_size=10*1024*1024, ping_interval=None) as ws:
        # Read challenge.
        await asyncio.wait_for(ws.recv(), timeout=3)

        # Handshake.
        connect = {
            'type': 'req', 'id': 'session-hs', 'method': 'connect',
            'params': {
                'minProtocol': 1, 'maxProtocol': 5,
                'client': {'id': 'dev-session', 'version': '1.0.0', 'platform': 'test', 'mode': 'control'}
            }
        }
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print('Handshake FAILED:', json.dumps(hello, indent=2))
            sys.exit(1)

        server_version = hello.get('payload', {}).get('server', {}).get('version', '?')
        print(f'==> Session connected (server {server_version})')
        print(f'    Sending {len(CMDS)} RPC calls on same connection')
        print()

        for i, cmd in enumerate(CMDS, 1):
            method = cmd['method']
            params = cmd['params']
            rpc_id = f'session-{i}-{int(time.time()*1000)}'
            rpc = {'type': 'req', 'id': rpc_id, 'method': method, 'params': params}
            await ws.send(json.dumps(rpc))

            # Collect response(s) - some RPCs send streaming events before the final response.
            responses = []
            while True:
                msg = json.loads(await asyncio.wait_for(ws.recv(), timeout=30))
                # Events (type=event) are streamed mid-call; collect them.
                if msg.get('type') == 'event':
                    responses.append(msg)
                    continue
                # Final response for this RPC.
                responses.append(msg)
                break

            ok = responses[-1].get('ok', False)
            status = 'OK' if ok else 'FAIL'
            print(f'  [{i}/{len(CMDS)}] {method} -> {status}')

            # Print events if any.
            for r in responses[:-1]:
                evt = r.get('event', '?')
                payload_str = json.dumps(r.get('payload', {}), ensure_ascii=False)
                if len(payload_str) > 200:
                    payload_str = payload_str[:200] + '...'
                print(f'         event: {evt} -> {payload_str}')

            # Print final response.
            final = responses[-1]
            payload = final.get('payload', final.get('error', {}))
            payload_str = json.dumps(payload, indent=2, ensure_ascii=False)
            # Truncate very long payloads.
            lines = payload_str.split('\n')
            if len(lines) > 30:
                payload_str = '\n'.join(lines[:30]) + f'\n  ... ({len(lines)-30} more lines)'
            print(f'         {payload_str}')
            print()

        print('==> Session complete')

asyncio.run(main())
"
}

# Send a chat message and stream the full multi-turn response (events + final).
# Usage: scripts/dev-live-test.sh chat "hello, what can you do?"
cmd_chat() {
  local message="${1:-}"
  if [[ -z "$message" ]]; then
    echo "Usage: scripts/dev-live-test.sh chat MESSAGE"
    return 1
  fi

  python3 -c "
import json, asyncio, sys, time, websockets

MESSAGE = $(python3 -c "import json; print(json.dumps('$message'))")

async def main():
    uri = 'ws://$DEV_HOST:$DEV_PORT/ws'
    async with websockets.connect(uri, max_size=10*1024*1024, ping_interval=None) as ws:
        # Challenge + handshake.
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {
            'type': 'req', 'id': 'chat-hs', 'method': 'connect',
            'params': {
                'minProtocol': 1, 'maxProtocol': 5,
                'client': {'id': 'dev-chat', 'version': '1.0.0', 'platform': 'test', 'mode': 'control'}
            }
        }
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print('Handshake FAILED:', json.dumps(hello, indent=2))
            sys.exit(1)

        # Create session.
        sess = f'dev-chat-{int(time.time()*1000)}'
        await ws.send(json.dumps({
            'type': 'req', 'id': 'chat-sess', 'method': 'sessions.create',
            'params': {'key': sess, 'kind': 'direct'}
        }))
        sess_resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        if not sess_resp.get('ok'):
            print('Session create FAILED:', json.dumps(sess_resp, indent=2))
            sys.exit(1)

        # Send chat message.
        rpc_id = f'chat-{int(time.time()*1000)}'
        rpc = {
            'type': 'req', 'id': rpc_id, 'method': 'chat.send',
            'params': {'sessionKey': sess, 'message': MESSAGE}
        }
        print(f'==> Sending chat: {MESSAGE[:80]}')
        await ws.send(json.dumps(rpc))

        # Stream all responses until we get the final response for our rpc_id.
        event_count = 0
        while True:
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=60)
            except asyncio.TimeoutError:
                print('  [TIMEOUT] No response after 60s')
                break

            msg = json.loads(raw)
            msg_type = msg.get('type', '?')

            if msg_type == 'event':
                event_count += 1
                evt = msg.get('event', '?')
                payload = msg.get('payload', {})

                # Streaming text chunks.
                if evt in ('chat.delta', 'chat.chunk', 'session.chunk'):
                    text = payload.get('text', payload.get('delta', payload.get('content', '')))
                    if text:
                        print(text, end='', flush=True)
                        continue

                # Tool calls.
                if evt in ('chat.tool_call', 'session.tool_call'):
                    tool = payload.get('name', payload.get('tool', '?'))
                    print(f'\n  [TOOL] {tool}', flush=True)
                    continue

                # Tool results.
                if evt in ('chat.tool_result', 'session.tool_result'):
                    result = json.dumps(payload, ensure_ascii=False)
                    if len(result) > 300:
                        result = result[:300] + '...'
                    print(f'\n  [TOOL_RESULT] {result}', flush=True)
                    continue

                # Status events.
                if evt in ('session.status', 'chat.status', 'session.transition'):
                    status = payload.get('status', payload.get('phase', json.dumps(payload)))
                    print(f'\n  [STATUS] {status}', flush=True)
                    continue

                # Other events - brief summary.
                payload_str = json.dumps(payload, ensure_ascii=False)
                if len(payload_str) > 150:
                    payload_str = payload_str[:150] + '...'
                print(f'\n  [EVENT:{evt}] {payload_str}', flush=True)
                continue

            # Final response.
            if msg.get('id') == rpc_id:
                ok = msg.get('ok', False)
                print()
                if ok:
                    payload = msg.get('payload', {})
                    reply = payload.get('reply', payload.get('message', payload.get('text', '')))
                    if reply:
                        print(f'==> Final reply ({event_count} events):')
                        print(reply[:2000])
                    else:
                        print(f'==> Response OK ({event_count} events):')
                        print(json.dumps(payload, indent=2, ensure_ascii=False)[:2000])
                else:
                    error = msg.get('error', {})
                    print(f'==> Chat FAILED: {json.dumps(error, indent=2)}')
                break

            # Response for a different ID (e.g. intermediate).
            print(f'\n  [RESPONSE:{msg.get(\"id\",\"?\")}] ok={msg.get(\"ok\")}', flush=True)

asyncio.run(main())
"
}

# Quality tests: response quality, formatting, Korean, tool usage, latency.
cmd_quality() {
  local scenario="${1:-all}"
  shift 2>/dev/null || true
  python3 "$SCRIPT_DIR/dev-quality-test.py" --port "$DEV_PORT" --scenario "$scenario" "$@"
}

cmd_quality_custom() {
  local message="${1:-}"
  if [[ -z "$message" ]]; then
    echo "Usage: scripts/dev-live-test.sh quality-custom MESSAGE"
    return 1
  fi
  shift
  python3 "$SCRIPT_DIR/dev-quality-test.py" --port "$DEV_PORT" --custom "$message" "$@"
}

cmd_quality_history() {
  python3 "$SCRIPT_DIR/dev-quality-test.py" --history "$@"
}

cmd_quality_compare() {
  python3 "$SCRIPT_DIR/dev-quality-test.py" --compare "$@"
}

cmd_quality_trend() {
  python3 "$SCRIPT_DIR/dev-quality-test.py" --trend "$@"
}

# --- Autoresearch integration ---
# Sends chat commands to the LLM agent, which invokes the autoresearch tool.
# This is the only way to drive autoresearch — it's an LLM tool, not a direct RPC.

# Metric script for autoresearch: builds gateway, runs smoke test, returns pass rate.
# Usage: scripts/dev-live-test.sh metric-script
# Outputs the script path that can be used as metric_cmd in autoresearch init.
cmd_metric_script() {
  local script="/tmp/deneb-autoresearch-metric.sh"
  cat > "$script" << 'METRIC_EOF'
#!/usr/bin/env bash
# Autoresearch metric: build gateway + smoke test.
# Returns pass count as the metric value.
set -euo pipefail
REPO_DIR="${AUTORESEARCH_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"
PORT="${DEV_LIVE_PORT:-18791}"
BINARY="/tmp/deneb-gateway-metric"
LOG="/tmp/deneb-gateway-metric.log"

# Build.
go build -C "$REPO_DIR/gateway-go" -ldflags "-s -w" -o "$BINARY" ./cmd/gateway/ 2>&1 || { echo "metric_value=0"; exit 0; }

# Start on isolated port.
"$BINARY" --bind loopback --port "$PORT" > "$LOG" 2>&1 &
PID=$!
trap "kill $PID 2>/dev/null; rm -f $BINARY" EXIT

# Wait for health.
for i in $(seq 1 30); do
  curl -sf "http://127.0.0.1:$PORT/health" > /dev/null 2>&1 && break
  sleep 0.2
done

# Run checks.
PASS=0
TOTAL=3

# 1. Health.
STATUS=$(curl -sf "http://127.0.0.1:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
[[ "$STATUS" == "ok" ]] && PASS=$((PASS+1))

# 2. Ready.
READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/ready" 2>/dev/null)
[[ "$READY" == "200" ]] && PASS=$((PASS+1))

# 3. WebSocket handshake + RPC.
WS_OK=$(python3 -c "
import json, asyncio, websockets
async def main():
    ws = await websockets.connect('ws://127.0.0.1:$PORT/ws', ping_interval=None)
    await asyncio.wait_for(ws.recv(), timeout=3)
    connect = {'type':'req','id':'m-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'metric','version':'1.0.0','platform':'test','mode':'control'}}}
    await ws.send(json.dumps(connect))
    hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
    if not hello.get('ok'): print('0'); return
    import time
    rpc = {'type':'req','id':f'metric-{int(time.time()*1000)}','method':'health','params':{}}
    await ws.send(json.dumps(rpc))
    resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
    print('1' if resp.get('ok') else '0')
    await ws.close()
asyncio.run(main())
" 2>/dev/null)
[[ "$WS_OK" == "1" ]] && PASS=$((PASS+1))

echo "metric_value=$PASS"
METRIC_EOF
  chmod +x "$script"
  echo "$script"
  echo "==> Metric script written to $script"
  echo "    Checks: health + ready + WebSocket RPC ($((3)) total)"
  echo "    Use as metric_cmd in autoresearch init"
}

# Ask the LLM agent to perform autoresearch actions via chat.send.
# Usage:
#   scripts/dev-live-test.sh ar-status          Check autoresearch status
#   scripts/dev-live-test.sh ar-results [FMT]   Get results (tsv|chart|summary)
#   scripts/dev-live-test.sh ar-chat "CMD"      Send arbitrary autoresearch instruction to LLM
cmd_ar_status() {
  _chat_and_wait "오토리서치 현재 상태 알려줘. status action 사용해"
}

cmd_ar_results() {
  local fmt="${1:-summary}"
  _chat_and_wait "오토리서치 결과 보여줘. results action으로, format=$fmt"
}

cmd_ar_chat() {
  local message="${1:-}"
  if [[ -z "$message" ]]; then
    echo "Usage: scripts/dev-live-test.sh ar-chat MESSAGE"
    return 1
  fi
  _chat_and_wait "$message"
}

# Internal: send chat message and wait for done event, printing streamed text.
_chat_and_wait() {
  local message="$1"
  python3 -c "
import json, asyncio, time, websockets

async def main():
    ws = await websockets.connect('ws://$DEV_HOST:$DEV_PORT/ws', max_size=10*1024*1024, ping_interval=None)
    await asyncio.wait_for(ws.recv(), timeout=3)
    connect = {'type':'req','id':'ar-hs','method':'connect','params':{'minProtocol':1,'maxProtocol':5,'client':{'id':'ar-test','version':'1.0.0','platform':'test','mode':'control'}}}
    await ws.send(json.dumps(connect))
    hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
    if not hello.get('ok'):
        print('Handshake FAILED')
        return

    # Create session.
    sess = f'ar-{int(time.time()*1000)}'
    await ws.send(json.dumps({'type':'req','id':'ar-sess','method':'sessions.create','params':{'key':sess,'kind':'direct'}}))
    await asyncio.wait_for(ws.recv(), timeout=5)

    # Send chat.
    run_id = f'ar-run-{int(time.time()*1000)}'
    msg = json.dumps($(python3 -c "import json; print(json.dumps('$message'))"))
    await ws.send(json.dumps({
        'type':'req','id':'ar-chat','method':'chat.send',
        'params':{'sessionKey':sess,'message':msg,'clientRunId':run_id}
    }))

    # Stream until done.
    start = time.time()
    text = ''
    for _ in range(2000):
        try:
            raw = await asyncio.wait_for(ws.recv(), timeout=30)
        except asyncio.TimeoutError:
            print(f'\n[TIMEOUT after {time.time()-start:.0f}s]')
            break
        frame = json.loads(raw)
        evt = frame.get('event','')
        payload = frame.get('payload',{})
        state = payload.get('state','')

        if evt == 'chat.delta':
            delta = payload.get('delta','')
            text += delta
            print(delta, end='', flush=True)
        elif evt == 'chat.tool':
            if state == 'started':
                print(f'\n  [TOOL] {payload.get(\"tool\",\"?\")}', flush=True)
            elif state == 'completed':
                result = payload.get('result','')
                if len(str(result)) > 300:
                    result = str(result)[:300] + '...'
                print(f'  [RESULT] {result}', flush=True)
        elif evt == 'chat' and state in ('done','error','aborted'):
            final_text = payload.get('text', text)
            usage = payload.get('usage',{})
            out_tok = usage.get('outputTokens', '?')
            print(f'\n\n==> {state.upper()} ({time.time()-start:.1f}s, {out_tok} tokens)')
            break
        elif evt == 'tick':
            continue

    await ws.close()

asyncio.run(main())
"
}

cmd_logs() {
  local n="${1:-50}"
  if [[ -f "$DEV_LOG" ]]; then
    tail -n "$n" "$DEV_LOG"
  else
    echo "No log file at $DEV_LOG"
  fi
}

# Follow logs in real-time (tail -f equivalent). Useful for background monitoring.
cmd_logs_watch() {
  if [[ -f "$DEV_LOG" ]]; then
    echo "==> Following $DEV_LOG (Ctrl+C to stop)"
    tail -f "$DEV_LOG"
  else
    echo "No log file at $DEV_LOG"
    return 1
  fi
}

# Search logs for a specific pattern.
cmd_logs_grep() {
  local pattern="${1:-}"
  if [[ -z "$pattern" ]]; then
    echo "Usage: scripts/dev-live-test.sh logs-grep PATTERN"
    return 1
  fi
  if [[ -f "$DEV_LOG" ]]; then
    grep -n --color=auto "$pattern" "$DEV_LOG" || echo "No matches for '$pattern'"
  else
    echo "No log file at $DEV_LOG"
  fi
}

# Show only error and warning lines.
cmd_logs_errors() {
  if [[ -f "$DEV_LOG" ]]; then
    grep -niE '"level":"(error|warn)"|ERROR|WARN|panic|fatal' "$DEV_LOG" | tail -n "${1:-50}" || echo "No errors/warnings found"
  else
    echo "No log file at $DEV_LOG"
  fi
}

# Show logs from the last N seconds.
cmd_logs_since() {
  local secs="${1:-60}"
  if [[ -f "$DEV_LOG" ]]; then
    local cutoff
    cutoff=$(date -d "-${secs} seconds" '+%Y-%m-%dT%H:%M:%S' 2>/dev/null || date -v-${secs}S '+%Y-%m-%dT%H:%M:%S' 2>/dev/null)
    if [[ -n "$cutoff" ]]; then
      awk -v cutoff="$cutoff" '$0 >= cutoff || /^[^0-9]/' "$DEV_LOG" | tail -n 200
    else
      echo "Date calculation not supported, showing last $secs lines instead"
      tail -n "$secs" "$DEV_LOG"
    fi
  else
    echo "No log file at $DEV_LOG"
  fi
}

# --- Internal helpers ---

_is_running() {
  [[ -f "$DEV_PID_FILE" ]] && kill -0 "$(cat "$DEV_PID_FILE")" 2>/dev/null
}

_ws_rpc() {
  local method="$1"
  local params="$2"
  python3 -c "
import json, asyncio, sys, time, websockets

async def main():
    uri = 'ws://$DEV_HOST:$DEV_PORT/ws'
    async with websockets.connect(uri, ping_interval=None) as ws:
        # Read challenge.
        await asyncio.wait_for(ws.recv(), timeout=3)

        # Handshake (type must be 'req' to match Go FrameTypeRequest).
        connect = {
            'type': 'req', 'id': 'dev-hs', 'method': 'connect',
            'params': {
                'minProtocol': 1, 'maxProtocol': 5,
                'client': {'id': 'dev-live-test', 'version': '1.0.0', 'platform': 'test', 'mode': 'control'}
            }
        }
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print(json.dumps(hello, indent=2))
            sys.exit(1)

        # RPC call.
        rpc_id = f'dev-rpc-{int(time.time()*1000)}'
        rpc = {'type': 'req', 'id': rpc_id, 'method': '$method', 'params': json.loads('$params')}
        await ws.send(json.dumps(rpc))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=10))
        print(json.dumps(resp, indent=2, ensure_ascii=False))

asyncio.run(main())
"
}

# --- Pre-parse global flags ---

ARGS=()
for arg in "$@"; do
  case "$arg" in
    --prod-parity) ;; # Ignored (prod config is now the default).
    *) ARGS+=("$arg") ;;
  esac
done
set -- "${ARGS[@]+"${ARGS[@]}"}"

# --- Main dispatch ---

case "${1:-help}" in
  build)       cmd_build ;;
  start)       cmd_start ;;
  stop)        cmd_stop ;;
  restart)     cmd_restart ;;
  status)      cmd_status ;;
  health)      cmd_health ;;
  smoke)       cmd_smoke ;;
  rpc)         shift; cmd_rpc "$@" ;;
  session)     shift; cmd_session "$@" ;;
  chat)           shift; cmd_chat "$@" ;;
  quality)          shift; cmd_quality "$@" ;;
  quality-custom)   shift; cmd_quality_custom "$@" ;;
  quality-list)     python3 "$SCRIPT_DIR/dev-quality-test.py" --list ;;
  quality-history)  shift; cmd_quality_history "$@" ;;
  quality-compare)  shift; cmd_quality_compare "$@" ;;
  quality-trend)    shift; cmd_quality_trend "$@" ;;
  metric-script)  cmd_metric_script ;;
  metric-gen)     shift; "$SCRIPT_DIR/dev-metric-gen.sh" "$@" ;;
  ar-start)       shift; "$SCRIPT_DIR/dev-autoresearch.sh" start "$@" ;;
  ar-stop)        "$SCRIPT_DIR/dev-autoresearch.sh" stop ;;
  ar-status)      "$SCRIPT_DIR/dev-autoresearch.sh" status ;;
  ar-results)     shift; "$SCRIPT_DIR/dev-ar-results.sh" "$@" ;;
  ar-suggest)     "$SCRIPT_DIR/dev-ar-results.sh" --suggest ;;
  ar-chat)        shift; cmd_ar_chat "$@" ;;
  logs)           shift; cmd_logs "$@" ;;
  logs-watch)  cmd_logs_watch ;;
  logs-grep)   shift; cmd_logs_grep "$@" ;;
  logs-errors) shift; cmd_logs_errors "$@" ;;
  logs-since)  shift; cmd_logs_since "$@" ;;

  # --- Reproduction commands (delegate to dev-reproduce.py) ---
  chat-check)  shift; python3 "$SCRIPT_DIR/dev-reproduce.py" --port "$DEV_PORT" chat-check "$@" ;;
  multi-chat)  shift; python3 "$SCRIPT_DIR/dev-reproduce.py" --port "$DEV_PORT" multi-chat "$@" ;;
  tool-check)  shift; python3 "$SCRIPT_DIR/dev-reproduce.py" --port "$DEV_PORT" tool-check "$@" ;;

  # --- Virtual Telegram (full Telegram pipeline testing) ---
  vchat)         shift; python3 "$SCRIPT_DIR/vchat.py" "$@" ;;
  vchat-start)   python3 "$SCRIPT_DIR/vchat.py" start --no-build ;;
  vchat-send)    shift; python3 "$SCRIPT_DIR/vchat.py" send "$@" ;;
  vchat-multi)   shift; python3 "$SCRIPT_DIR/vchat.py" multi "$@" ;;
  vchat-stop)    python3 "$SCRIPT_DIR/vchat.py" stop ;;
  vchat-status)  python3 "$SCRIPT_DIR/vchat.py" status ;;
  vchat-reset)   python3 "$SCRIPT_DIR/vchat.py" reset ;;
  vchat-timeline) python3 "$SCRIPT_DIR/vchat.py" timeline ;;
  vchat-logs)    shift; python3 "$SCRIPT_DIR/vchat.py" logs "$@" ;;

  # vchat quality testing (Telegram pipeline quality checks — mock).
  vchat-quality)       shift; python3 "$SCRIPT_DIR/dev-vchat-quality.py" "$@" ;;
  vchat-quality-custom) shift; python3 "$SCRIPT_DIR/dev-vchat-quality.py" --custom "$@" ;;

  # Real Telegram e2e quality testing (Telethon — actual Telegram servers).
  e2e-quality)         shift; python3 "$SCRIPT_DIR/dev-e2e-quality.py" "$@" ;;
  e2e-quality-custom)  shift; python3 "$SCRIPT_DIR/dev-e2e-quality.py" --custom "$@" ;;

  # Baseline tracking.
  baseline)      shift; "$SCRIPT_DIR/dev-baseline.sh" "$@" ;;
  baseline-save) "$SCRIPT_DIR/dev-baseline.sh" save ;;
  baseline-compare) "$SCRIPT_DIR/dev-baseline.sh" compare ;;

  # Parity report.
  parity)        cmd_parity ;;

  help|*)
    echo "Usage: scripts/dev-live-test.sh COMMAND [ARGS]"
    echo ""
    echo "Lifecycle:"
    echo "  build           Build gateway binary from current tree"
    echo "  start           Start dev gateway on port $DEV_PORT"
    echo "  stop            Stop dev gateway"
    echo "  restart         Rebuild + restart"
    echo "  status          Show dev gateway status + health"
    echo ""
    echo "Testing:"
    echo "  health              GET /health (JSON)"
    echo "  smoke               Full smoke test (health + ready + WS RPC)"
    echo "  rpc M [P]           Single RPC call (new connection per call)"
    echo "  session C1 C2..     Multi-turn: multiple RPCs on one connection"
    echo "  chat MSG            Send chat message, stream full response"
    echo "  quality [SCENARIO]  Quality test (300 cases, YAML-driven)"
    echo "    Scenarios: all|core|health|daily|system|code|task|search|knowledge"
    echo "               format|context|edge|safety|korean|persona|reasoning"
    echo "    Legacy:    chat|tools|tools-deep (aliases for new categories)"
    echo "    Flags:     --record (save to DB), --model MODEL (override)"
    echo "  quality-custom MSG  Quality test with custom message"
    echo "  quality-list        List all available quality tests"
    echo "  quality-history     Show past quality test runs"
    echo "  quality-compare A B Compare two runs side-by-side"
    echo "  quality-trend NAME  Show score trend for a test"
    echo ""
    echo "Reproduction (for AI agents to reproduce user-reported symptoms):"
    echo "  chat-check MSG [--expect PAT] [--expect-not PAT] [--expect-tool TOOL]"
    echo "                      Chat + assertions (Korean, latency, patterns, tools)"
    echo "  multi-chat M1 M2..  Multi-turn chat on same session (context carryover)"
    echo "  tool-check TOOL MSG Verify specific tool invocation"
    echo ""
    echo "Virtual Telegram (full Telegram pipeline, Claude Code가 직접 사용자 역할):"
    echo "  vchat start         Mock Telegram + Gateway 시작 (foreground)"
    echo "  vchat-start         위와 동일 (--no-build)"
    echo "  vchat-send MSG      메시지 전송 + 전체 텔레그램 경험 렌더링"
    echo "  vchat-multi M1 M2.. 멀티턴 대화 (세션 유지)"
    echo "  vchat-stop          정지"
    echo "  vchat-status        실행 상태"
    echo "  vchat-reset         대화 초기화 (서버 유지)"
    echo "  vchat-timeline      전체 타임라인 출력"
    echo "  vchat-logs [-n N]   게이트웨이 로그"
    echo "  vchat-quality [S]   텔레그램 파이프라인 품질 테스트 (korean|tool|format|multi|all)"
    echo "  vchat-quality-custom MSG  커스텀 메시지 품질 테스트"
    echo ""
    echo "Real Telegram E2E (실제 텔레그램 서버 경유, Telethon 기반):"
    echo "  e2e-quality [S]     실제 텔레그램 e2e 품질 테스트 (korean|tool|format|multi|all)"
    echo "  e2e-quality-custom MSG  커스텀 메시지 e2e 테스트"
    echo "    Flags: --bot USERNAME (default: DENEB_DEV_BOT_USERNAME), --json"
    echo ""
    echo "Baseline (regression detection):"
    echo "  baseline save       현재 결과를 베이스라인으로 저장"
    echo "  baseline compare    현재 결과 vs 베이스라인 비교"
    echo "  baseline show       현재 브랜치 베이스라인 표시"
    echo "  baseline-save       baseline save 단축"
    echo "  baseline-compare    baseline compare 단축"
    echo ""
    echo "Autoresearch:"
    echo "  metric-gen [PRESET] 메트릭 스크립트 생성 (smoke|quality|vchat|combined|custom)"
    echo "  metric-script       레거시: smoke 메트릭 스크립트 생성"
    echo "  ar-start [OPTS]     오토리서치 시작 (--target FILE --metric PRESET)"
    echo "  ar-stop             오토리서치 정지"
    echo "  ar-status           오토리서치 상태 확인"
    echo "  ar-results [FMT]    결과 조회 (--json|--table|--best|--failures|--suggest)"
    echo "  ar-suggest          다음 행동 제안"
    echo "  ar-chat MSG         LLM에게 오토리서치 지시 전송"
    echo ""
    echo "Logs:"
    echo "  logs [N]        Tail last N log lines (default 50)"
    echo "  logs-watch      Follow logs in real-time (tail -f)"
    echo "  logs-grep PAT   Search logs for pattern"
    echo "  logs-errors [N] Show only error/warning lines (last N, default 50)"
    echo "  logs-since SECS Show logs from last N seconds"
    echo ""
    echo "Parity:"
    echo "  parity              Show dev vs production environment differences"
    echo ""
    echo "Config: always uses production config (via dev-config-gen.sh)."
    echo "Telegram: set DENEB_DEV_TELEGRAM_TOKEN in ~/.deneb/.env to enable dev bot."
    ;;
esac
