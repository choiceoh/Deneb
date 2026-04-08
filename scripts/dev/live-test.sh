#!/usr/bin/env bash
# Live test helper for Claude Code development workflow.
#
# All chat/quality/reproduction tests use real Telegram (Telethon).
# WebSocket test path has been removed — tests verify the actual user experience.
#
# Usage:
#   scripts/live-test.sh build              Build gateway from current tree
#   scripts/live-test.sh start              Start dev gateway on port 18790
#   scripts/live-test.sh stop               Stop dev gateway
#   scripts/live-test.sh restart            Rebuild + restart
#   scripts/live-test.sh status             Check if dev gateway is running
#   scripts/live-test.sh health             Hit /health endpoint
#   scripts/live-test.sh smoke              Smoke test (health + ready)
#   scripts/live-test.sh chat MESSAGE       Send chat via Telegram, wait for response
#   scripts/live-test.sh quality [SCENARIO]  Run quality tests (165 cases, Telegram)
#   scripts/live-test.sh quality-custom MSG Quality test with custom message
#   scripts/live-test.sh quality-list       List all available quality tests
#   scripts/live-test.sh quality-history    Show past quality test runs
#   scripts/live-test.sh quality-compare A B Compare two runs
#   scripts/live-test.sh quality-trend NAME  Score trend for a test
#
# Benchmarks (Arena-Hard + MT-Bench + Oolong + LLM-as-Judge + Pairwise):
#   scripts/live-test.sh bench [SUITE]       Run benchmark tests (all/challenge/multiturn/oolong)
#   scripts/live-test.sh bench-judge MSG     LLM-as-Judge single message evaluation
#
#   scripts/live-test.sh model [show|list|set MODEL]  Hot-swap model without restart
#
#   scripts/live-test.sh logs [N]           Tail dev gateway logs (default: 50 lines)
#   scripts/live-test.sh logs-watch         Follow dev gateway logs in real-time
#   scripts/live-test.sh logs-grep PATTERN  Search logs for pattern
#   scripts/live-test.sh logs-errors        Show only error/warning lines from logs
#   scripts/live-test.sh logs-since SECS    Show logs from last N seconds
#
# Reproduction (AI agent reproduces user-reported symptoms via Telegram):
#   scripts/live-test.sh chat-check MSG [--expect PAT] [--expect-tool TOOL] ...
#   scripts/live-test.sh multi-chat MSG1 MSG2 MSG3 [--expect-context PAT]
#   scripts/live-test.sh tool-check TOOL_NAME MSG
#
# The dev instance runs on port 18790 (separate from production on 18789).
#
# Config: always uses production config with dev bot token (via config-gen.sh).
# This exercises the same code paths as production (providers, auth, hooks,
# agents, sessions, logging) with a separate Telegram bot to avoid 409 conflicts.

set -euo pipefail

# Source shared dev server library.
source "$(cd "$(dirname "$0")" && pwd)/lib-server.sh"
devlib_load_dotenv

SCRIPT_DIR="$DEVLIB_SCRIPT_DIR"
REPO_DIR="$DEVLIB_REPO_DIR"
DEV_PORT="${DEV_LIVE_PORT:-18790}"
DEV_BINARY="/tmp/deneb-gateway-live"
DEV_PID_FILE="/tmp/deneb-gateway-live.pid"
DEV_LOG="/tmp/deneb-gateway-live.log"
DEV_HOST="$DEVLIB_HOST"
DEV_STATE_DIR="/tmp/deneb-dev-state"
DENEB_VERSION=$(devlib_version)

cmd_build() {
  echo "==> Building gateway from $(basename "$REPO_DIR")..."
  devlib_build "$DEV_BINARY"
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

  local dev_config="/tmp/deneb-dev-config.json"
  devlib_gen_config "$dev_config"
  if [[ -n "${DENEB_DEV_TELEGRAM_TOKEN:-}" ]]; then
    echo "    Config: production (Telegram: dev bot active)"
  else
    echo "    Config: production (Telegram: disabled, set DENEB_DEV_TELEGRAM_TOKEN)"
  fi

  echo "==> Starting dev gateway on $DEV_HOST:$DEV_PORT..."
  devlib_start_gateway "$DEV_BINARY" "$DEV_PORT" "$dev_config" "$DEV_STATE_DIR" "$DEV_LOG" nohup
  echo "$DEVLIB_PID" > "$DEV_PID_FILE"

  if devlib_wait_healthy "$DEV_HOST" "$DEV_PORT" 25; then
    echo "    Running (PID $DEVLIB_PID, port $DEV_PORT)"
    return 0
  fi

  echo "    WARN: Gateway started but /health not responding after 6s"
  echo "    Check logs: scripts/live-test.sh logs"
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
  devlib_stop_pid "$pid"
  rm -f "$DEV_PID_FILE"

  if devlib_wait_port_free "$DEV_PORT"; then
    echo "    Stopped"
  else
    echo "    WARN: Port $DEV_PORT still in use"
  fi
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
  echo "==> Smoke test against $DEV_HOST:$DEV_PORT"

  # Run 2 HTTP checks in parallel.
  local _tmp="/tmp/deneb-livetest-smoke-$$"

  (curl -sf "http://$DEV_HOST:$DEV_PORT/health" 2>/dev/null \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null \
    || echo "") > "$_tmp-h" &
  local pid_h=$!

  (curl -sf -o /dev/null -w "%{http_code}" "http://$DEV_HOST:$DEV_PORT/ready" 2>/dev/null \
    || echo "000") > "$_tmp-r" &
  local pid_r=$!

  wait $pid_h $pid_r

  # Evaluate results.
  local failed=0

  local status
  status=$(cat "$_tmp-h" 2>/dev/null || echo "")
  echo -n "  [1/2] GET /health ... "
  if [[ "$status" == "ok" ]]; then echo "OK"; else echo "FAIL (status=$status)"; failed=1; fi

  local ready_code
  ready_code=$(cat "$_tmp-r" 2>/dev/null || echo "000")
  echo -n "  [2/2] GET /ready ... "
  if [[ "$ready_code" == "200" ]]; then echo "OK"; else echo "FAIL (HTTP $ready_code)"; failed=1; fi

  rm -f "$_tmp-h" "$_tmp-r"

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

  # 2. Core backend.
  echo "--- Core Backend ---"
  echo "  [OK]   Pure Go (Rust core removed)"
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

  # 5. Storage isolation.
  echo "--- Storage ---"
  echo "  [OK]   Dev state dir: $DEV_STATE_DIR (isolated from ~/.deneb)"
  echo "  [OK]   Wiki/diary: $DEV_STATE_DIR/wiki, $DEV_STATE_DIR/memory/diary"
  echo ""

  # 6. Port/binding.
  echo "--- Network ---"
  echo "  [INFO] Dev port: $DEV_PORT (production: 18789)"
  echo "  [INFO] Dev bind: loopback (production: config-driven)"
  echo ""

  # 7. Summary.
  if (( issues == 0 )); then
    echo "==> No parity gaps detected"
  else
    echo "==> $issues parity gap(s) found"
  fi
}

# Send a chat message via Telegram and show the response.
# Usage: scripts/live-test.sh chat "hello, what can you do?"
cmd_chat() {
  local message="${1:-}"
  if [[ -z "$message" ]]; then
    echo "Usage: scripts/live-test.sh chat MESSAGE"
    return 1
  fi

  python3 -c "
import asyncio, sys, time
sys.path.insert(0, '$SCRIPT_DIR')
from telegram_test_client import TelegramTestClient, check_prerequisites

async def main():
    ok, detail = check_prerequisites()
    if not ok:
        print(f'Telegram prerequisites not met: {detail}')
        sys.exit(1)

    client = TelegramTestClient()
    bot = await client.connect()
    print(f'Connected to {bot}')
    print(f'==> Sending: $(echo "$message" | head -c 80)')

    capture = await client.chat('''$message''')
    await client.disconnect()

    if capture.reply_text:
        print()
        print(capture.reply_text[:2000])
        print()
        print(f'==> Done ({capture.latency_ms:.0f}ms, {len(capture.draft_edits)} edits)')
    elif capture.errors:
        print(f'==> FAILED: {capture.errors}')
    else:
        print('==> No response')

asyncio.run(main())
"
}

# Quality tests: response quality, formatting, Korean, tool usage, latency.
cmd_quality() {
  local scenario="${1:-all}"
  shift 2>/dev/null || true
  python3 "$SCRIPT_DIR/quality-test.py" --port "$DEV_PORT" --scenario "$scenario" "$@"
}

cmd_quality_custom() {
  local message="${1:-}"
  if [[ -z "$message" ]]; then
    echo "Usage: scripts/live-test.sh quality-custom MESSAGE"
    return 1
  fi
  shift
  python3 "$SCRIPT_DIR/quality-test.py" --port "$DEV_PORT" --custom "$message" "$@"
}

cmd_quality_history() {
  python3 "$SCRIPT_DIR/quality-test.py" --history "$@"
}

cmd_quality_compare() {
  python3 "$SCRIPT_DIR/quality-test.py" --compare "$@"
}

cmd_quality_trend() {
  python3 "$SCRIPT_DIR/quality-test.py" --trend "$@"
}

# --- Autoresearch integration ---
# Delegates to autoresearch.sh and ar-results.sh scripts.

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
    echo "Usage: scripts/live-test.sh logs-grep PATTERN"
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

# --- Model hot-swap ---

cmd_model() {
  local sub="${1:-show}"
  shift 2>/dev/null || true

  case "$sub" in
    show|get)
      local resp
      resp=$(curl -sf "http://$DEV_HOST:$DEV_PORT/admin/model" 2>/dev/null) || {
        echo "ERROR: dev gateway not responding (port $DEV_PORT)"
        return 1
      }
      local current
      current=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin)['current'])" 2>/dev/null)
      echo "현재 모델: $current"
      ;;

    list)
      local resp
      resp=$(curl -sf "http://$DEV_HOST:$DEV_PORT/admin/model" 2>/dev/null) || {
        echo "ERROR: dev gateway not responding (port $DEV_PORT)"
        return 1
      }
      python3 -c "
import sys, json
data = json.load(sys.stdin)
print(f'현재: {data[\"current\"]}')
print()
for m in data.get('available', []):
    marker = ' ✓' if m['full_id'] == data['current'] else ''
    print(f'  [{m[\"role\"]}] {m[\"full_id\"]}{marker}')
" <<< "$resp"
      ;;

    set|switch)
      local model="${1:-}"
      if [[ -z "$model" ]]; then
        echo "Usage: scripts/live-test.sh model set MODEL"
        echo "  예: model set zai/glm-5-turbo"
        echo "      model set google/gemini-3.1-pro-preview"
        echo "      model set main  (역할 이름도 가능)"
        return 1
      fi
      local resp
      resp=$(curl -sf -X PUT "http://$DEV_HOST:$DEV_PORT/admin/model" \
        -H "Content-Type: application/json" \
        -d "{\"model\":\"$model\"}" 2>/dev/null) || {
        echo "ERROR: dev gateway not responding (port $DEV_PORT)"
        return 1
      }
      local prev current
      prev=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin)['previous'])" 2>/dev/null)
      current=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin)['current'])" 2>/dev/null)
      echo "모델 변경: $prev → $current"
      ;;

    *)
      echo "Usage: scripts/live-test.sh model [show|list|set MODEL]"
      echo ""
      echo "  show          현재 모델 표시 (기본)"
      echo "  list          사용 가능한 모델 목록"
      echo "  set MODEL     모델 핫스왑 (재시작 없음)"
      ;;
  esac
}

# --- Internal helpers ---

_is_running() {
  [[ -f "$DEV_PID_FILE" ]] && kill -0 "$(cat "$DEV_PID_FILE")" 2>/dev/null
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
  chat)           shift; cmd_chat "$@" ;;
  quality)          shift; cmd_quality "$@" ;;
  quality-custom)   shift; cmd_quality_custom "$@" ;;
  quality-list)     python3 "$SCRIPT_DIR/quality-test.py" --list ;;
  quality-history)  shift; cmd_quality_history "$@" ;;
  quality-compare)  shift; cmd_quality_compare "$@" ;;
  quality-trend)    shift; cmd_quality_trend "$@" ;;
  # metric-gen removed: use iterate.sh --metric PRESET directly.
  ar-start)       shift; "$SCRIPT_DIR/autoresearch.sh" start "$@" ;;
  ar-stop)        "$SCRIPT_DIR/autoresearch.sh" stop ;;
  ar-status)      "$SCRIPT_DIR/autoresearch.sh" status ;;
  ar-results)     shift; "$SCRIPT_DIR/ar-results.sh" "$@" ;;
  ar-suggest)     "$SCRIPT_DIR/ar-results.sh" --suggest ;;
  logs)           shift; cmd_logs "$@" ;;
  logs-watch)  cmd_logs_watch ;;
  logs-grep)   shift; cmd_logs_grep "$@" ;;
  logs-errors) shift; cmd_logs_errors "$@" ;;
  logs-since)  shift; cmd_logs_since "$@" ;;

  # --- Reproduction commands (delegate to reproduce.py via Telegram) ---
  chat-check)  shift; python3 "$SCRIPT_DIR/reproduce.py" --port "$DEV_PORT" chat-check "$@" ;;
  multi-chat)  shift; python3 "$SCRIPT_DIR/reproduce.py" --port "$DEV_PORT" multi-chat "$@" ;;
  tool-check)  shift; python3 "$SCRIPT_DIR/reproduce.py" --port "$DEV_PORT" tool-check "$@" ;;

  # Benchmarks (Arena-Hard / MT-Bench / Oolong / LLM-as-Judge / Pairwise).
  bench)
    shift
    BENCH_SUITE="${1:-all}"
    shift || true
    case "$BENCH_SUITE" in
      all)        SCENARIO="bench" ;;
      challenge)  SCENARIO="bench-ch" ;;
      multiturn)  SCENARIO="bench-mt" ;;
      oolong)     SCENARIO="bench-ool" ;;
      *)          SCENARIO="bench-$BENCH_SUITE" ;;
    esac
    echo "==> Running benchmark suite: $BENCH_SUITE (scenario=$SCENARIO)"
    python3 "$SCRIPT_DIR/quality-test.py" --scenario "$SCENARIO" --port "$DEV_PORT" \
      ${BOT_FLAG:+--bot "$BOT_FLAG"} "$@"
    ;;
  bench-judge)
    shift
    MSG="${1:?Usage: bench-judge MESSAGE}"
    shift || true
    echo "==> LLM-as-Judge evaluation"
    # Get response via Telegram, then score with LLM judge.
    python3 -c "
import sys, asyncio, importlib.util
sys.path.insert(0, '$SCRIPT_DIR')
from telegram_test_client import TelegramTestClient, check_prerequisites

async def main():
    ok, d = check_prerequisites()
    if not ok:
        print(f'ERROR: {d}')
        sys.exit(1)
    c = TelegramTestClient()
    await c.connect()
    cap = await c.chat(sys.argv[1])
    await c.disconnect()
    if not cap.reply_text:
        print('ERROR: No response captured')
        sys.exit(1)
    # Load bench-judge module.
    spec = importlib.util.spec_from_file_location('bench_judge', '$SCRIPT_DIR/bench-judge.py')
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    if not mod.judge_available():
        print('ERROR: No JUDGE_API_KEY or ANTHROPIC_API_KEY set')
        sys.exit(1)
    scores = mod.judge_absolute(sys.argv[1], cap.reply_text)
    overall = sum(scores.values()) / len(scores) * 10
    print(f'Overall: {overall:.0f}/100')
    for k, v in scores.items():
        print(f'  {k}: {v}/10')
" "$MSG"
    ;;

  # Baseline tracking.
  baseline)      shift; "$SCRIPT_DIR/baseline.sh" "$@" ;;
  baseline-save) "$SCRIPT_DIR/baseline.sh" save ;;
  baseline-compare) "$SCRIPT_DIR/baseline.sh" compare ;;

  # Model hot-swap (재시작 없이 모델 변경).
  model)         shift; cmd_model "$@" ;;

  # Parity report.
  parity)        cmd_parity ;;

  help|*)
    echo "Usage: scripts/live-test.sh COMMAND [ARGS]"
    echo ""
    echo "Lifecycle:"
    echo "  build           Build gateway binary from current tree"
    echo "  start           Start dev gateway on port $DEV_PORT"
    echo "  stop            Stop dev gateway"
    echo "  restart         Rebuild + restart"
    echo "  status          Show dev gateway status + health"
    echo ""
    echo "Testing (Telegram 기반 — 실제 유저 경험 경로 검증):"
    echo "  health              GET /health (JSON)"
    echo "  smoke               Smoke test (health + ready)"
    echo "  chat MSG            텔레그램으로 채팅 메시지 전송, 응답 확인"
    echo "  quality [SCENARIO]  품질 테스트 (165 cases, Telegram)"
    echo "    Scenarios: all|core|health|daily|system|code|task|search|knowledge"
    echo "               format|context|edge|safety|korean|persona|reasoning"
    echo "               bench-challenge|bench-multiturn|bench-oolong|bench (all bench)"
    echo "    Legacy:    chat|tools|tools-deep (aliases for new categories)"
    echo "    Flags:     --record (save to DB), --model MODEL, --bot USERNAME"
    echo "  quality-custom MSG  커스텀 메시지 품질 테스트"
    echo "  quality-list        테스트 목록 보기"
    echo "  quality-history     품질 테스트 이력"
    echo "  quality-compare A B 두 실행 비교"
    echo "  quality-trend NAME  점수 추이"
    echo ""
    echo "Reproduction (AI 에이전트 증상 재현, Telegram 기반):"
    echo "  chat-check MSG [--expect PAT] [--expect-not PAT] [--expect-tool TOOL]"
    echo "                      채팅 + assertion (Korean, latency, patterns, tools)"
    echo "  multi-chat M1 M2..  멀티턴 채팅 (컨텍스트 유지 확인)"
    echo "  tool-check TOOL MSG 특정 도구 호출 검증"
    echo ""
    echo "Baseline (regression detection):"
    echo "  baseline save       현재 결과를 베이스라인으로 저장"
    echo "  baseline compare    현재 결과 vs 베이스라인 비교"
    echo "  baseline show       현재 브랜치 베이스라인 표시"
    echo "  baseline-save       baseline save 단축"
    echo "  baseline-compare    baseline compare 단축"
    echo ""
    echo "Benchmarks (Arena-Hard + MT-Bench + Oolong + LLM-as-Judge + Pairwise):"
    echo "  bench [SUITE]       벤치마크 실행 (all|challenge|multiturn|oolong)"
    echo "  bench-judge MSG     LLM-as-Judge로 단일 메시지 품질 평가"
    echo "                      Requires JUDGE_API_KEY or ANTHROPIC_API_KEY"
    echo ""
    echo "Autoresearch:"
    echo "  ar-start [OPTS]     오토리서치 시작 (--target FILE --metric PRESET)"
    echo "  ar-stop             오토리서치 정지"
    echo "  ar-status           오토리서치 상태 확인"
    echo "  ar-results [FMT]    결과 조회 (--json|--table|--best|--failures|--suggest)"
    echo "  ar-suggest          다음 행동 제안"
    echo ""
    echo "Logs:"
    echo "  logs [N]        Tail last N log lines (default 50)"
    echo "  logs-watch      Follow logs in real-time (tail -f)"
    echo "  logs-grep PAT   Search logs for pattern"
    echo "  logs-errors [N] Show only error/warning lines (last N, default 50)"
    echo "  logs-since SECS Show logs from last N seconds"
    echo ""
    echo "Model (핫스왑 — 재시작 없이 모델 변경):"
    echo "  model               현재 모델 표시"
    echo "  model list          사용 가능한 모델 목록"
    echo "  model set MODEL     모델 핫스왑 (예: zai/glm-5-turbo, main, fallback)"
    echo ""
    echo "Parity:"
    echo "  parity              Show dev vs production environment differences"
    echo ""
    echo "Config: always uses production config (via config-gen.sh)."
    echo "Telegram: set DENEB_DEV_TELEGRAM_TOKEN in ~/.deneb/.env to enable dev bot."
    echo "테스트 전제조건: TELEGRAM_API_ID, TELEGRAM_API_HASH, ~/.deneb/telegram-test.session"
    ;;
esac
