#!/usr/bin/env bash
# puppet.sh — run the dev gateway with its LLM replaced by a coding agent.
#
# "빙의 모드": every LLM call the dev gateway makes is routed to a local
# puppet broker (scripts/dev/puppet_broker.py) that HOLDS the request until
# the operator answers it. The operator — typically a coding agent working on
# Deneb — therefore sees exactly what Deneb's model sees (assembled system
# prompt, message history, tool schemas) and drives the turn by choosing the
# text / tool calls. Tool calls are executed for real by the gateway.
#
# No gateway code changes: config-gen.sh output is overlaid with a
# models.providers.puppet entry + agents.*Model role overrides, then passed
# via DENEB_CONFIG_PATH (the same plumbing live-test.sh uses).
#
# Usage:
#   scripts/dev/puppet.sh start [--main-only] [--rebuild]
#   scripts/dev/puppet.sh send "메시지" [--new-session] [--sync]
#   scripts/dev/puppet.sh pending [--wait N]      # poll for held LLM requests
#   scripts/dev/puppet.sh show ID [--full|--raw]  # inspect prompt/messages/tools
#   scripts/dev/puppet.sh reply ID --text "..." [--tool NAME ARGS_JSON]...
#   scripts/dev/puppet.sh fail ID [--message M]   # abort with a provider error
#   scripts/dev/puppet.sh result                  # output of the last send
#   scripts/dev/puppet.sh history|status|logs|logs-broker|stop|restart
#
# Interop: the gateway uses the same binary/pid/log/state paths as
# live-test.sh (per DENEB_INSTANCE), so live-test.sh logs/logs-errors/status
# work against a puppet gateway. live-test.sh stop stops the gateway but not
# the broker; use puppet.sh stop to tear down both.
#
# Budget: miniapp.chat.send bounds the WHOLE turn at 5 minutes
# (DefaultTurnDeadline). Answer pending requests promptly — a held request
# whose turn deadline fires shows up as status=gone.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/lib-server.sh"
devlib_load_dotenv

SCRIPT_DIR="$DEVLIB_SCRIPT_DIR"
DEV_PORT="${DEV_LIVE_PORT:-$DEVLIB_LIVE_PORT}"
DEV_BINARY="${DEVLIB_TMP_PREFIX}-gateway-live"
DEV_PID_FILE="${DEVLIB_TMP_PREFIX}-gateway-live.pid"
DEV_LOG="${DEVLIB_TMP_PREFIX}-gateway-live.log"
DEV_HOST="$DEVLIB_HOST"
DEV_STATE_DIR="${DEVLIB_TMP_PREFIX}-dev-state"

MOCK_TELEGRAM_TOKEN="mock-dev-token"
MOCK_TELEGRAM_PORT="${DENEB_DEV_MOCK_TELEGRAM_PORT:-$DEVLIB_MOCK_DEFAULT_PORT}"
export DENEB_DEV_MOCK_TELEGRAM_URL="${DENEB_DEV_MOCK_TELEGRAM_URL:-http://$DEV_HOST:$MOCK_TELEGRAM_PORT}"

# Broker port: 4th slot of the lib-server.sh instance port block
# (default instance: prod=18789 dev=18790 iterate=18791 mock=18792 → 18793).
PUPPET_PORT="${DENEB_PUPPET_PORT:-$DEVLIB_PUPPET_PORT}"
PUPPET_URL="http://$DEV_HOST:$PUPPET_PORT"
PUPPET_PID_FILE="${DEVLIB_TMP_PREFIX}-puppet-broker.pid"
PUPPET_LOG="${DEVLIB_TMP_PREFIX}-puppet-broker.log"
PUPPET_JOURNAL="${DEVLIB_TMP_PREFIX}-puppet-journal.jsonl"
PUPPET_CONFIG="${DEVLIB_TMP_PREFIX}-puppet-config.json"
# Each send gets its own output/pid file (concurrent sends must not clobber
# each other); this pointer file names the most recent one for cmd_result.
PUPPET_SEND_LAST="${DEVLIB_TMP_PREFIX}-puppet-send.last"
PUPPET_MODEL="agent-seat"

# Operator CLI + chat injection reach their servers through these.
export DENEB_PUPPET_URL="$PUPPET_URL"
export DENEB_LIVETEST_GW_URL="http://$DEV_HOST:$DEV_PORT"
export DENEB_LIVETEST_STATE_DIR="$DEV_STATE_DIR"

_gateway_running() {
  [[ -f "$DEV_PID_FILE" ]] && devlib_is_pid_alive "$(cat "$DEV_PID_FILE")"
}

_broker_running() {
  curl -sf "$PUPPET_URL/puppet/health" >/dev/null 2>&1
}

_start_broker() {
  if _broker_running; then return 0; fi
  rm -f "$PUPPET_PID_FILE"
  nohup python3 "$SCRIPT_DIR/puppet_broker.py" serve \
    --host "$DEV_HOST" --port "$PUPPET_PORT" --model "$PUPPET_MODEL" \
    --journal "$PUPPET_JOURNAL" > "$PUPPET_LOG" 2>&1 &
  echo $! > "$PUPPET_PID_FILE"
  local retries=0
  while (( retries < 30 )); do
    _broker_running && return 0
    devlib_is_pid_alive "$(cat "$PUPPET_PID_FILE")" || return 1
    sleep 0.1; retries=$((retries + 1))
  done
  return 1
}

_stop_broker() {
  [[ -f "$PUPPET_PID_FILE" ]] || return 0
  devlib_stop_pid "$(cat "$PUPPET_PID_FILE" 2>/dev/null || echo "")"
  rm -f "$PUPPET_PID_FILE"
}

# Overlay the generated dev config: add the puppet provider and point the
# model roles at it. PUPPET_ALL_ROLES=1 possesses every LLM role (airtight:
# any stray background call surfaces in `pending` instead of silently going
# to a real model); --main-only limits to the chat main role, leaving
# lightweight/tiny/analysis/fallback on their production models.
_overlay_config() {
  PUPPET_CONFIG="$PUPPET_CONFIG" BROKER_URL="$PUPPET_URL" \
  PUPPET_MODEL="$PUPPET_MODEL" PUPPET_ALL_ROLES="$1" python3 - <<'PYEOF'
import json, os

path = os.environ["PUPPET_CONFIG"]
model = "puppet/" + os.environ["PUPPET_MODEL"]
with open(path) as f:
    cfg = json.load(f)

providers = cfg.setdefault("models", {}).setdefault("providers", {})
providers["puppet"] = {
    "baseUrl": os.environ["BROKER_URL"] + "/v1",
    "apiKey": "puppet-local",
    "api": "openai",
    "contextWindow": 200000,
}

agents = cfg.setdefault("agents", {})
agents["defaultModel"] = model
if os.environ.get("PUPPET_ALL_ROLES") == "1":
    for key in ("lightweightModel", "fallbackModel", "tinyModel",
                "analysisModel"):
        agents[key] = model
    # Subagents read agents.defaults.subagents.model when set; keep them in
    # the seat too rather than letting them slip to a real model.
    sub = agents.get("defaults", {}).get("subagents")
    if isinstance(sub, dict) and sub.get("model"):
        sub["model"] = model

with open(path, "w") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")
print(path)
PYEOF
}

cmd_start() {
  local all_roles=1 rebuild=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --main-only) all_roles=0; shift ;;
      --rebuild)   rebuild=1; shift ;;
      *) echo "unknown flag: $1" >&2; return 1 ;;
    esac
  done

  # config-gen.sh reads DENEB_CONFIG_PATH as the PRODUCTION source. If the
  # operator's shell still exports it pointing at a previously *generated*
  # dev/puppet config, generation would feed on its own output (and a later
  # live-test.sh start would inherit puppet roles and hang). Custom non-/tmp
  # paths are left alone — pointing config-gen at a bespoke base config is a
  # supported workflow.
  if [[ "${DENEB_CONFIG_PATH:-}" == /tmp/deneb*config*.json ]]; then
    echo "    WARN: ignoring DENEB_CONFIG_PATH=${DENEB_CONFIG_PATH} (generated dev config, not a production source)"
    unset DENEB_CONFIG_PATH
  fi

  if _gateway_running; then
    echo "==> Dev gateway already running — restarting with puppet config..."
    devlib_stop_pid "$(cat "$DEV_PID_FILE")"
    rm -f "$DEV_PID_FILE"
    devlib_wait_port_free "$DEV_PORT" || true
  fi

  if [[ "$rebuild" == "1" || ! -x "$DEV_BINARY" ]]; then
    echo "==> Building gateway..."
    devlib_build "$DEV_BINARY"
  fi

  echo "==> Starting puppet broker on $PUPPET_URL..."
  if ! _start_broker; then
    echo "    FAIL: broker did not start (log: $PUPPET_LOG)"
    return 1
  fi

  # The gateway's startup still expects the mock Telegram endpoint wiring
  # that live-test.sh provides; keep parity. Not fatal (the Telegram plugin
  # was retired in PR #1922), but a failure usually means a port conflict —
  # surface it instead of swallowing.
  if ! devlib_start_mock_telegram "$MOCK_TELEGRAM_PORT" "$DEV_HOST" >/dev/null; then
    echo "    WARN: mock Telegram server failed to start (log: $DEVLIB_MOCK_LOG) — continuing"
  fi

  echo "==> Generating puppet config (production config + puppet provider)..."
  DENEB_DEV_TELEGRAM_TOKEN="$MOCK_TELEGRAM_TOKEN" devlib_gen_config "$PUPPET_CONFIG"
  _overlay_config "$all_roles" >/dev/null
  if [[ "$all_roles" == "1" ]]; then
    echo "    Roles: ALL LLM roles → puppet/$PUPPET_MODEL"
  else
    echo "    Roles: main → puppet/$PUPPET_MODEL (others on production models)"
  fi

  echo "==> Starting dev gateway on $DEV_HOST:$DEV_PORT (puppet seat)..."
  DENEB_DEV_TELEGRAM_TOKEN="$MOCK_TELEGRAM_TOKEN" \
    devlib_start_gateway "$DEV_BINARY" "$DEV_PORT" "$PUPPET_CONFIG" \
      "$DEV_STATE_DIR" "$DEV_LOG" nohup
  echo "$DEVLIB_PID" > "$DEV_PID_FILE"

  if ! devlib_wait_healthy "$DEV_HOST" "$DEV_PORT" 25; then
    echo "    WARN: gateway started but /health not responding (logs: $DEV_LOG)"
    return 1
  fi
  echo "    Running (PID $DEVLIB_PID, port $DEV_PORT)"
  cat <<EOF

You are now Deneb's model. Typical loop:
  scripts/dev/puppet.sh send "안녕"          # inject a user message (async)
  scripts/dev/puppet.sh pending --wait 60   # wait for the LLM request
  scripts/dev/puppet.sh show r1             # read prompt/messages/tools
  scripts/dev/puppet.sh reply r1 --text "..."            # ...or...
  scripts/dev/puppet.sh reply r1 --tool fs '{"action":"list","path":"."}'
  scripts/dev/puppet.sh result              # final reply the user got
Turn budget: 5 minutes per send (DefaultTurnDeadline) — answer promptly.
EOF
}

cmd_send() {
  local message="${1:-}"
  shift || true
  if [[ -z "$message" ]]; then
    echo "Usage: puppet.sh send MESSAGE [--new-session] [--sync] [--timeout SECS]" >&2
    return 1
  fi
  # Instance-scoped default session: gateway transcripts/agent-logs live in
  # shared home dirs (~/.deneb/...), NOT the instance state dir — a fixed
  # global key would cross-contaminate parallel worktree instances.
  local session="client:puppet-${DEVLIB_INSTANCE}" sync=0 timeout=330
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --new-session) session="client:puppet-${DEVLIB_INSTANCE}-$(date +%s)-$$"; shift ;;
      --session)     session="${2:?--session requires a value}"; shift 2 ;;
      --sync)        sync=1; shift ;;
      --timeout)     timeout="${2:?--timeout requires a value}"; shift 2 ;;
      *) echo "unknown flag: $1" >&2; return 1 ;;
    esac
  done

  if ! _gateway_running; then
    echo "gateway not running — scripts/dev/puppet.sh start" >&2
    return 1
  fi

  # Same preflight live-test.sh chat runs: clear diagnostics for unreachable
  # gateway / missing client token, instead of a cryptic error buried in the
  # async result file.
  export DENEB_SCRIPTS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
  if ! python3 - <<'PYEOF'
import os, sys, urllib.parse
sys.path.insert(0, os.environ["DENEB_SCRIPTS_DIR"])
from mock_native_client import check_prerequisites
u = urllib.parse.urlparse(os.environ.get("DENEB_LIVETEST_GW_URL") or "")
ok, detail = check_prerequisites(u.hostname or "127.0.0.1", u.port or 18790)
if not ok:
    print(f"Native chat prerequisites not met: {detail}", file=sys.stderr)
    raise SystemExit(1)
PYEOF
  then
    return 1
  fi

  local runner
  runner=$(cat <<'PYEOF'
import asyncio, json, os, sys, time

sys.path.insert(0, os.environ["DENEB_SCRIPTS_DIR"])
from mock_native_client import NativeTestClient

async def main():
    client = NativeTestClient()
    await client.connect()
    cap = await client.chat(
        os.environ["MOCK_MESSAGE"],
        timeout=float(os.environ.get("MOCK_TIMEOUT", "330")),
        session_key=os.environ.get("MOCK_SESSION", ""),
    )
    print(json.dumps({
        "reply": cap.reply_text,
        "errors": cap.errors,
        "latencyMs": round(cap.latency_ms),
        "usage": cap.token_usage,
        "session": os.environ.get("MOCK_SESSION", ""),
        "finishedAt": time.strftime("%H:%M:%S"),
    }, ensure_ascii=False, indent=2))

asyncio.run(main())
PYEOF
)
  export MOCK_MESSAGE="$message" MOCK_SESSION="$session" MOCK_TIMEOUT="$timeout"
  if [[ "$sync" == "1" ]]; then
    python3 -c "$runner"
    return $?
  fi
  # Per-send files: a second send must not truncate the first one's output.
  local out="${DEVLIB_TMP_PREFIX}-puppet-send-$$.out"
  nohup python3 -c "$runner" > "$out" 2>&1 &
  echo $! > "${out%.out}.pid"
  printf '%s\n' "$out" > "$PUPPET_SEND_LAST"
  echo "sent (session=$session, pid $(cat "${out%.out}.pid"), out $out)"
  echo "next: scripts/dev/puppet.sh pending --wait 60"
}

cmd_result() {
  local out=""
  if [[ -f "$PUPPET_SEND_LAST" ]]; then
    out="$(cat "$PUPPET_SEND_LAST")"
  fi
  if [[ -z "$out" || ! -f "$out" ]]; then
    echo "(no send yet)"
    return 1
  fi
  local pidf="${out%.out}.pid"
  if [[ -f "$pidf" ]] && devlib_is_pid_alive "$(cat "$pidf")"; then
    echo "(turn still running — gateway has not returned yet)"
  fi
  cat "$out"
}

cmd_status() {
  if _gateway_running; then
    echo "gateway: RUNNING (PID $(cat "$DEV_PID_FILE"), port $DEV_PORT, config $PUPPET_CONFIG)"
  else
    echo "gateway: STOPPED"
  fi
  if _broker_running; then
    echo "broker:  RUNNING ($PUPPET_URL)"
    curl -sf "$PUPPET_URL/puppet/state" 2>/dev/null || true
    echo ""
  else
    echo "broker:  STOPPED"
  fi
}

cmd_stop() {
  if _gateway_running; then
    echo "==> Stopping dev gateway..."
    devlib_stop_pid "$(cat "$DEV_PID_FILE")"
    rm -f "$DEV_PID_FILE"
  fi
  echo "==> Stopping puppet broker..."
  _stop_broker
  if devlib_mock_telegram_running "$MOCK_TELEGRAM_PORT"; then
    devlib_stop_mock_telegram
  fi
  # /tmp is tmpfs on the DGX hosts — don't leave per-send result files behind.
  rm -f "${DEVLIB_TMP_PREFIX}"-puppet-send-*.out \
        "${DEVLIB_TMP_PREFIX}"-puppet-send-*.pid "$PUPPET_SEND_LAST"
  echo "stopped"
}

cmd_help() { sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; }

case "${1:-help}" in
  start)       shift; cmd_start "$@" ;;
  restart)     shift; cmd_stop >/dev/null; cmd_start "$@" ;;
  stop)        cmd_stop ;;
  status)      cmd_status ;;
  send)        shift; cmd_send "$@" ;;
  result)      cmd_result ;;
  logs)        tail -n "${2:-50}" "$DEV_LOG" ;;
  logs-broker) tail -n "${2:-50}" "$PUPPET_LOG" ;;
  pending|show|reply|fail|history)
    python3 "$SCRIPT_DIR/puppet_broker.py" "$@" ;;
  help|--help|-h) cmd_help ;;
  *) echo "unknown command: $1 (try: puppet.sh help)" >&2; exit 1 ;;
esac
