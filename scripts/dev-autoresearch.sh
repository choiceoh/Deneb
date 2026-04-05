#!/usr/bin/env bash
# One-shot autoresearch wrapper for Claude Code agents.
#
# Sends autoresearch commands through the gateway's chat.send RPC,
# which triggers the autoresearch tool inside the LLM agent.
#
# Usage:
#   scripts/dev-autoresearch.sh start --target FILE [OPTIONS]
#   scripts/dev-autoresearch.sh status
#   scripts/dev-autoresearch.sh results [--json|--table]
#   scripts/dev-autoresearch.sh stop
#
# Start options:
#   --target FILE           Target file(s) to optimize (required, comma-separated)
#   --metric PRESET         Metric preset: smoke|quality|vchat|combined (default: smoke)
#   --scenario SCENARIO     vchat scenario (default: all)
#   --name NAME             Metric name (default: auto from preset)
#   --direction DIR         maximize|minimize (default: maximize)
#   --budget SECS           Time budget per experiment (default: 120)
#   --iterations N          Max iterations (default: 20)
#   --tag TAG               Branch tag (default: auto)
#   --constants SPEC        Constants mode: "NAME:TYPE:MIN:MAX,..." (optional)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Gateway connection (use production or dev port).
GW_HOST="127.0.0.1"
GW_PORT="${DEV_LIVE_PORT:-18790}"

CMD="${1:-help}"
shift || true

# --- Chat helper: send a message and capture tool results ---
_chat_rpc() {
  local message="$1"
  local timeout="${2:-120}"
  python3 -c "
import json, asyncio, time, sys
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
        print(f'ERROR: cannot connect to gateway at $GW_HOST:$GW_PORT: {e}', file=sys.stderr)
        sys.exit(1)

    try:
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'ar-hs','method':'connect','params':{
            'minProtocol':1,'maxProtocol':5,
            'client':{'id':'ar-wrapper','version':'1.0.0','platform':'test','mode':'control'}
        }}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print('ERROR: handshake failed', file=sys.stderr)
            sys.exit(1)

        # Create session.
        sess = f'ar-{int(time.time()*1000)}'
        await ws.send(json.dumps({'type':'req','id':'ar-sess','method':'sessions.create',
            'params':{'key':sess,'kind':'direct'}}))
        await asyncio.wait_for(ws.recv(), timeout=5)

        # Send chat.
        run_id = f'ar-run-{int(time.time()*1000)}'
        await ws.send(json.dumps({
            'type':'req','id':'ar-chat','method':'chat.send',
            'params':{'sessionKey':sess,'message':$(python3 -c "import json; print(json.dumps('''$message'''))"),'clientRunId':run_id}
        }))

        # Collect response.
        text = ''
        tool_results = []
        start = time.time()
        for _ in range(2000):
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=$timeout)
            except asyncio.TimeoutError:
                break
            frame = json.loads(raw)
            evt = frame.get('event','')
            payload = frame.get('payload',{})
            state = payload.get('state','')

            if evt == 'chat.delta':
                text += payload.get('delta','')
            elif evt == 'chat.tool':
                if state == 'completed':
                    tool_results.append({
                        'tool': payload.get('tool',''),
                        'result': payload.get('result',''),
                        'isError': payload.get('isError', False),
                    })
            elif evt == 'chat' and state in ('done','error','aborted'):
                text = payload.get('text', text)
                break
            elif evt == 'tick':
                continue

        # Output: text on stdout, tool results as JSON on stderr.
        print(text)
        if tool_results:
            for tr in tool_results:
                r = str(tr.get('result',''))
                print(f'TOOL_RESULT {json.dumps(tr, ensure_ascii=False)}', file=sys.stderr)

    finally:
        await ws.close()

asyncio.run(main())
"
}

# --- Commands ---

cmd_start() {
  local targets="" metric="smoke" scenario="all" name="" direction="maximize"
  local budget=120 iterations=20 tag="" constants=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --target) targets="$2"; shift 2 ;;
      --metric) metric="$2"; shift 2 ;;
      --scenario) scenario="$2"; shift 2 ;;
      --name) name="$2"; shift 2 ;;
      --direction) direction="$2"; shift 2 ;;
      --budget) budget="$2"; shift 2 ;;
      --iterations) iterations="$2"; shift 2 ;;
      --tag) tag="$2"; shift 2 ;;
      --constants) constants="$2"; shift 2 ;;
      *) echo "Unknown option: $1"; exit 1 ;;
    esac
  done

  if [[ -z "$targets" ]]; then
    echo "ERROR: --target is required"
    echo "Usage: dev-autoresearch.sh start --target FILE [OPTIONS]"
    exit 1
  fi

  # Generate metric script.
  local metric_script
  case "$metric" in
    smoke)    metric_script=$("$SCRIPT_DIR/dev-metric-gen.sh" smoke) ;;
    quality)  metric_script=$("$SCRIPT_DIR/dev-metric-gen.sh" quality) ;;
    vchat)    metric_script=$("$SCRIPT_DIR/dev-metric-gen.sh" vchat --scenario "$scenario") ;;
    combined) metric_script=$("$SCRIPT_DIR/dev-metric-gen.sh" combined) ;;
    *)
      # Treat as raw command.
      metric_script="$metric"
      ;;
  esac

  # Auto-derive metric name if not set.
  [[ -z "$name" ]] && name="$metric"
  [[ -z "$tag" ]] && tag="$metric-$(date +%m%d-%H%M)"

  echo "autoresearch: starting"
  echo "  target: $targets"
  echo "  metric: $metric ($metric_script)"
  echo "  direction: $direction"
  echo "  budget: ${budget}s/experiment"
  echo "  iterations: $iterations"
  echo "  tag: $tag"

  # Build the instruction for the LLM agent.
  local target_list
  target_list=$(echo "$targets" | tr ',' ' ' | sed 's/ /", "/g')

  local constants_json=""
  if [[ -n "$constants" ]]; then
    # Parse "NAME:TYPE:MIN:MAX,..." into JSON.
    constants_json=$(python3 -c "
import json
specs = '''$constants'''.split(',')
result = []
for spec in specs:
    parts = spec.strip().split(':')
    if len(parts) >= 4:
        result.append({'name': parts[0], 'type': parts[1], 'min': parts[2], 'max': parts[3]})
    elif len(parts) >= 2:
        result.append({'name': parts[0], 'type': parts[1]})
print(json.dumps(result))
")
    echo "  constants: $constants_json"
  fi

  local msg="오토리서치 시작해줘. autoresearch 도구를 사용해서:
action: init
workdir: $REPO_DIR
target_files: [\"$target_list\"]
metric_cmd: bash $metric_script
metric_name: $name
metric_direction: $direction
time_budget_sec: $budget
max_iterations: $iterations
branch_tag: $tag"

  if [[ -n "$constants_json" ]]; then
    msg="$msg
constants: $constants_json"
  fi

  msg="$msg

init이 완료되면 바로 action: start로 시작해."

  echo ""
  _chat_rpc "$msg" 30
}

cmd_status() {
  echo "autoresearch: checking status..."
  _chat_rpc "오토리서치 현재 상태 알려줘. autoresearch 도구의 status action 사용해. workdir은 $REPO_DIR" 15
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

  # For JSON, try to parse results directly from the .autoresearch dir first.
  if [[ "$format" == "json" ]]; then
    "$SCRIPT_DIR/dev-ar-results.sh" --json
    return
  fi

  echo "autoresearch: fetching results ($format)..."
  _chat_rpc "오토리서치 결과 보여줘. autoresearch 도구의 results action, format=$format, workdir=$REPO_DIR" 15
}

cmd_stop() {
  echo "autoresearch: stopping..."
  _chat_rpc "오토리서치 멈춰줘. autoresearch 도구의 stop action 사용해." 15
}

# --- Main ---
case "$CMD" in
  start)   cmd_start "$@" ;;
  status)  cmd_status ;;
  results) cmd_results "$@" ;;
  stop)    cmd_stop ;;
  *)
    echo "Usage: dev-autoresearch.sh {start|status|results|stop}"
    echo ""
    echo "Start options:"
    echo "  --target FILE         Target file(s) (required, comma-separated)"
    echo "  --metric PRESET       smoke|quality|vchat|combined (default: smoke)"
    echo "  --scenario SCENARIO   vchat scenario: korean|tool|format|multi|all"
    echo "  --direction DIR       maximize|minimize (default: maximize)"
    echo "  --budget SECS         Time per experiment (default: 120)"
    echo "  --iterations N        Max iterations (default: 20)"
    echo "  --tag TAG             Branch tag (default: auto)"
    echo "  --constants SPEC      Constants: NAME:TYPE:MIN:MAX,..."
    exit 1
    ;;
esac
