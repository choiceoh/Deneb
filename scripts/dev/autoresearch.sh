#!/usr/bin/env bash
# Autoresearch wrapper — direct RPC (no LLM bypass).
#
# Calls autoresearch RPC methods directly via WebSocket,
# instead of sending chat messages through the LLM.
#
# Usage:
#   scripts/autoresearch.sh start --target FILE [OPTIONS]
#   scripts/autoresearch.sh status
#   scripts/autoresearch.sh results [--json|--table]
#   scripts/autoresearch.sh stop
#
# Start options:
#   --target FILE           Target file(s) to optimize (required, comma-separated)
#   --metric PRESET         Metric preset: smoke|quality|combined (default: smoke)
#   --name NAME             Metric name (default: auto from preset)
#   --direction DIR         maximize|minimize (default: maximize)
#   --budget SECS           Time budget per experiment (default: 120)
#   --iterations N          Max iterations (default: 20)
#   --tag TAG               Branch tag (default: auto)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

GW_HOST="127.0.0.1"
GW_PORT="${DEV_LIVE_PORT:-18790}"

CMD="${1:-help}"
shift || true

# --- Direct WebSocket RPC helper ---
# Connects, handshakes, sends ONE RPC request, prints the response payload.
_rpc() {
  local method="$1"
  local params="${2:-{}}"
  python3 -c "
import json, asyncio, sys
try:
    import websockets
except ImportError:
    print('ERROR: pip install websockets', file=sys.stderr)
    sys.exit(1)

async def main():
    try:
        ws = await asyncio.wait_for(
            websockets.connect('ws://$GW_HOST:$GW_PORT/ws', max_size=10*1024*1024, ping_interval=None),
            timeout=5)
    except Exception as e:
        print(json.dumps({'ok': False, 'error': f'cannot connect to $GW_HOST:$GW_PORT: {e}'}))
        sys.exit(1)

    try:
        # Read server hello.
        await asyncio.wait_for(ws.recv(), timeout=3)
        # Handshake.
        await ws.send(json.dumps({
            'type': 'req', 'id': 'hs', 'method': 'connect',
            'params': {'minProtocol': 1, 'maxProtocol': 5,
                       'client': {'id': 'ar-cli', 'version': '1.0.0', 'platform': 'script', 'mode': 'control'}}
        }))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print(json.dumps({'ok': False, 'error': 'handshake failed'}))
            sys.exit(1)

        # Send RPC request.
        await ws.send(json.dumps({
            'type': 'req', 'id': 'ar-rpc', 'method': '$method',
            'params': json.loads('''$params''')
        }))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=30))
        print(json.dumps(resp, ensure_ascii=False))
    finally:
        await ws.close()

asyncio.run(main())
"
}

# --- Commands ---

cmd_start() {
  local targets="" metric="smoke" name="" direction="maximize"
  local budget=120 iterations=20 tag=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --target) targets="$2"; shift 2 ;;
      --metric) metric="$2"; shift 2 ;;
      --name) name="$2"; shift 2 ;;
      --direction) direction="$2"; shift 2 ;;
      --budget) budget="$2"; shift 2 ;;
      --iterations) iterations="$2"; shift 2 ;;
      --tag) tag="$2"; shift 2 ;;
      *) echo "Unknown option: $1"; exit 1 ;;
    esac
  done

  if [[ -z "$targets" ]]; then
    echo "ERROR: --target is required"
    echo "Usage: autoresearch.sh start --target FILE [OPTIONS]"
    exit 1
  fi

  # Build metric command from preset.
  local metric_cmd
  case "$metric" in
    smoke)    metric_cmd="$SCRIPT_DIR/iterate.sh --metric smoke" ;;
    quality)  metric_cmd="$SCRIPT_DIR/iterate.sh --metric quality" ;;
    combined) metric_cmd="$SCRIPT_DIR/iterate.sh --metric combined" ;;
    *)        metric_cmd="$metric" ;;
  esac

  [[ -z "$name" ]] && name="$metric"
  [[ -z "$tag" ]] && tag="$metric-$(date +%m%d-%H%M)"

  echo "autoresearch: configuring + starting"
  echo "  target: $targets"
  echo "  metric: $name ($metric_cmd)"
  echo "  direction: $direction"
  echo "  budget: ${budget}s/experiment"
  echo "  iterations: $iterations"
  echo "  tag: $tag"

  # Build target_files JSON array.
  local target_json
  target_json=$(python3 -c "import json; print(json.dumps([f.strip() for f in '''$targets'''.split(',')]))")

  # Step 1: Configure.
  local params
  params=$(python3 -c "
import json
d = {
    'workdir': '$REPO_DIR',
    'target_files': $target_json,
    'metric_cmd': '$metric_cmd',
    'metric_name': '$name',
    'metric_direction': '$direction',
    'time_budget_sec': $budget,
    'max_iterations': $iterations,
    'branch_tag': '$tag',
}
print(json.dumps(d))
")

  local resp
  resp=$(_rpc "autoresearch.config" "$params")
  local ok
  ok=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('ok', False))" 2>/dev/null)
  if [[ "$ok" != "True" ]]; then
    echo "ERROR: config failed"
    echo "$resp" | python3 -m json.tool 2>/dev/null || echo "$resp"
    exit 1
  fi
  echo "  configured"

  # Step 2: Start.
  resp=$(_rpc "autoresearch.start" "{\"workdir\": \"$REPO_DIR\"}")
  ok=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('ok', False))" 2>/dev/null)
  if [[ "$ok" != "True" ]]; then
    echo "ERROR: start failed"
    echo "$resp" | python3 -m json.tool 2>/dev/null || echo "$resp"
    exit 1
  fi
  echo "  started"
}

cmd_status() {
  local resp
  resp=$(_rpc "autoresearch.status" "{}")
  echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
p = d.get('payload', d)
running = p.get('running', False)
print(f'autoresearch: {\"RUNNING\" if running else \"STOPPED\"}')
if running or p.get('total_iterations', 0) > 0:
    print(f'  target: {\", \".join(p.get(\"target_files\", []))}')
    print(f'  metric: {p.get(\"metric_name\", \"?\")} ({p.get(\"metric_direction\", \"?\")})')
    total = p.get('total_iterations', 0)
    kept = p.get('kept_iterations', 0)
    print(f'  iterations: {total} (kept: {kept})')
    if p.get('baseline_metric') is not None:
        print(f'  baseline: {p[\"baseline_metric\"]}')
    if p.get('best_metric') is not None:
        print(f'  best: {p[\"best_metric\"]}')
    consec = p.get('consecutive_failures', 0)
    if consec > 0:
        print(f'  consecutive failures: {consec}')
" 2>/dev/null || echo "ERROR: cannot parse response"
}

cmd_results() {
  local format="summary"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --json)  format="json"; shift ;;
      --table) format="tsv"; shift ;;
      *) format="$1"; shift ;;
    esac
  done

  # For JSON, use ar-results.sh (works without gateway).
  if [[ "$format" == "json" ]]; then
    "$SCRIPT_DIR/ar-results.sh" --json
    return
  fi

  local resp
  resp=$(_rpc "autoresearch.results" "{\"workdir\": \"$REPO_DIR\", \"format\": \"$format\"}")
  echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
p = d.get('payload', d)
if 'tsv' in p:
    print(p['tsv'])
elif 'summary' in p:
    print(p['summary'])
else:
    print(json.dumps(p, indent=2, ensure_ascii=False))
" 2>/dev/null || echo "$resp"
}

cmd_stop() {
  echo "autoresearch: stopping..."
  local resp
  resp=$(_rpc "autoresearch.stop" "{}")
  echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
p = d.get('payload', d)
total = p.get('total_iterations', 0)
kept = p.get('kept_iterations', 0)
print(f'  stopped (iterations: {total}, kept: {kept})')
if p.get('best_metric') is not None:
    print(f'  best: {p[\"best_metric\"]} (baseline: {p.get(\"baseline_metric\", \"?\")})')
" 2>/dev/null || echo "  stopped"
}

# --- Main ---
case "$CMD" in
  start)   cmd_start "$@" ;;
  status)  cmd_status ;;
  results) cmd_results "$@" ;;
  stop)    cmd_stop ;;
  *)
    echo "Usage: autoresearch.sh {start|status|results|stop}"
    echo ""
    echo "Start options:"
    echo "  --target FILE         Target file(s) (required, comma-separated)"
    echo "  --metric PRESET       smoke|quality|combined (default: smoke)"
    echo "  --direction DIR       maximize|minimize (default: maximize)"
    echo "  --budget SECS         Time per experiment (default: 120)"
    echo "  --iterations N        Max iterations (default: 20)"
    echo "  --tag TAG             Branch tag (default: auto)"
    exit 1
    ;;
esac
