#!/usr/bin/env bash
# Metric script generator for autoresearch.
#
# Generates self-contained metric scripts that build, test, and output
# metric_value=N for use as autoresearch metric_cmd.
#
# Presets:
#   smoke     — Health + Ready (metric_value=0~2)
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

# Source shared dev server library.
source "$(cd "$(dirname "$0")" && pwd)/lib-dev-server.sh"

SCRIPT_DIR="$DEVLIB_SCRIPT_DIR"
REPO_DIR="$DEVLIB_REPO_DIR"
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
REPO_DIR="${AUTORESEARCH_REPO:-PLACEHOLDER_REPO}"
PORT="${METRIC_PORT:-18791}"
BINARY="/tmp/deneb-gateway-metric"
LOG="/tmp/deneb-gateway-metric.log"
HOST="127.0.0.1"

source "$REPO_DIR/scripts/lib-dev-server.sh"
trap 'devlib_stop_pid "${GW_PID:-}"' EXIT

# Build.
if ! devlib_build "$BINARY" 2>/tmp/deneb-metric-build.log; then
  echo "metric_value=0"
  echo "DENEB_METRIC_DETAIL build=fail"
  exit 0
fi
HEADER_EOF
  sed -i "s|PLACEHOLDER_REPO|$REPO_DIR|g" "$script"
}

_write_server_start() {
  local script="$1"
  cat >> "$script" << 'START_EOF'

# Start gateway.
DEV_CONFIG="/tmp/deneb-metric-config.json"
devlib_gen_config "$DEV_CONFIG"
devlib_start_gateway "$BINARY" "$PORT" "$DEV_CONFIG" "/tmp/deneb-metric-state" "$LOG"
GW_PID=$DEVLIB_PID

if ! devlib_wait_healthy "$HOST" "$PORT" 30; then
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

# Smoke checks (health + ready).
PASS=0

# 1. Health.
STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
[[ "$STATUS" == "ok" ]] && PASS=$((PASS+1))

# 2. Ready.
READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
[[ "$READY" == "200" ]] && PASS=$((PASS+1))

echo "metric_value=$PASS"
echo "DENEB_METRIC_DETAIL build=ok server=ok health=$([[ $STATUS == ok ]] && echo ok || echo fail) ready=$([[ $READY == 200 ]] && echo ok || echo fail)"
SMOKE_EOF

  chmod +x "$script"
  echo "$script"
}

gen_quality() {
  local script="$OUT_DIR/deneb-metric-quality.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'QUALITY_EOF'

# Quality metric via Telegram (delegates to dev-quality-metric.sh).
QUALITY_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" 2>&1) || true
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
# Smoke (health + ready).
SMOKE_PASS=0
STATUS=$(curl -sf "http://$HOST:$PORT/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
[[ "$STATUS" == "ok" ]] && SMOKE_PASS=$((SMOKE_PASS+1))
READY=$(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null || echo "000")
[[ "$READY" == "200" ]] && SMOKE_PASS=$((SMOKE_PASS+1))

# If smoke fails completely, return 0.
if [[ "$SMOKE_PASS" -eq 0 ]]; then
  echo "metric_value=0"
  echo "DENEB_METRIC_DETAIL build=ok smoke=0/2 quality=skip"
  exit 0
fi

# Quality (via Telegram).
QUALITY_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" 2>&1) || true
QUALITY_VAL=$(echo "$QUALITY_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
QUALITY_VAL=${QUALITY_VAL:-0}

# Combined: smoke_score(0~20) + quality_score(0~80).
SMOKE_SCORE=$(python3 -c "print(round($SMOKE_PASS / 2 * 20))")
QUALITY_SCORE=$(python3 -c "print(round($QUALITY_VAL / 100 * 80))")
TOTAL=$(python3 -c "print($SMOKE_SCORE + $QUALITY_SCORE)")

echo "metric_value=$TOTAL"
echo "DENEB_METRIC_DETAIL build=ok smoke=$SMOKE_PASS/2($SMOKE_SCORE) quality=$QUALITY_VAL($QUALITY_SCORE) combined=$TOTAL"
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

gen_judge() {
  local script="$OUT_DIR/deneb-metric-judge.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'JUDGE_EOF'

# LLM-as-Judge quality metric via Telegram.
# Sends a chat message, captures response, then uses LLM to evaluate quality.
# Requires JUDGE_API_KEY or ANTHROPIC_API_KEY.
MESSAGE="${JUDGE_MESSAGE:-안녕, 이 서버에 대해 간단히 설명해줘}"

# 1) Get chat response via Telegram quality metric.
QUALITY_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" "$MESSAGE" 2>&1) || true
HEURISTIC_VAL=$(echo "$QUALITY_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
HEURISTIC_VAL=${HEURISTIC_VAL:-0}

# 2) Extract response text from quality metric stderr for judge input.
RESPONSE_TEXT=$(echo "$QUALITY_OUT" | grep '  reply: ' | sed 's/^  reply: //' | head -1)

# 3) Run LLM judge on (message, response).
if [[ -n "${JUDGE_API_KEY:-${ANTHROPIC_API_KEY:-}}" ]] && [[ -n "$RESPONSE_TEXT" ]]; then
  JUDGE_OUT=$(python3 "$REPO_DIR/scripts/dev-bench-judge.py" absolute \
    --message "$MESSAGE" --response "$RESPONSE_TEXT" 2>&1) || true
  JUDGE_VAL=$(echo "$JUDGE_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
  JUDGE_DETAIL=$(echo "$JUDGE_OUT" | grep '^DENEB_JUDGE_DETAIL ' | tail -1)
  JUDGE_VAL=${JUDGE_VAL:-0}

  # Combined: heuristic(30%) + judge(70%)
  TOTAL=$(python3 -c "print(round($HEURISTIC_VAL * 0.3 + $JUDGE_VAL * 0.7))")
  echo "metric_value=$TOTAL"
  echo "DENEB_METRIC_DETAIL build=ok server=ok heuristic=$HEURISTIC_VAL judge=$JUDGE_VAL combined=$TOTAL"
  [[ -n "$JUDGE_DETAIL" ]] && echo "$JUDGE_DETAIL"
else
  # Fallback to heuristic only.
  echo "metric_value=$HEURISTIC_VAL"
  echo "DENEB_METRIC_DETAIL build=ok server=ok heuristic=$HEURISTIC_VAL judge=skipped"
fi
JUDGE_EOF

  chmod +x "$script"
  echo "$script"
}

gen_pairwise() {
  local script="$OUT_DIR/deneb-metric-pairwise.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'PAIRWISE_EOF'

# Pairwise comparison metric for autoresearch.
# Sends same message to baseline and candidate, LLM picks winner.
# Requires JUDGE_API_KEY or ANTHROPIC_API_KEY.
# Requires PAIRWISE_BASELINE_PORT (default: 18789, production gateway).
MESSAGE="${PAIRWISE_MESSAGE:-시스템 상태 확인하고 간단히 요약해줘}"
BASELINE_PORT="${PAIRWISE_BASELINE_PORT:-18789}"

# 1) Get candidate response (current build on $PORT).
CAND_OUT=$("$REPO_DIR/scripts/dev-quality-metric.sh" "$MESSAGE" 2>&1) || true
CAND_TEXT=$(echo "$CAND_OUT" | grep '  reply: ' | sed 's/^  reply: //' | head -1)

# 2) Get baseline response (production on $BASELINE_PORT).
# Use a separate quality metric call pointed at baseline port.
BASE_OUT=$(METRIC_PORT=$BASELINE_PORT "$REPO_DIR/scripts/dev-quality-metric.sh" "$MESSAGE" 2>&1) || true
BASE_TEXT=$(echo "$BASE_OUT" | grep '  reply: ' | sed 's/^  reply: //' | head -1)

# 3) Pairwise judge.
if [[ -n "${JUDGE_API_KEY:-${ANTHROPIC_API_KEY:-}}" ]] && [[ -n "$CAND_TEXT" ]] && [[ -n "$BASE_TEXT" ]]; then
  PW_OUT=$(python3 "$REPO_DIR/scripts/dev-bench-judge.py" pairwise \
    --message "$MESSAGE" --response-a "$BASE_TEXT" --response-b "$CAND_TEXT" 2>&1) || true
  # B=candidate wins → 100, tie → 50, A=baseline wins → 0
  PW_METRIC=$(echo "$PW_OUT" | grep -oP 'metric_value=\K[\d.]+' | tail -1)
  PW_DETAIL=$(echo "$PW_OUT" | grep '^DENEB_JUDGE_DETAIL ' | tail -1)
  PW_METRIC=${PW_METRIC:-50}
  echo "metric_value=$PW_METRIC"
  echo "DENEB_METRIC_DETAIL build=ok server=ok mode=pairwise"
  [[ -n "$PW_DETAIL" ]] && echo "$PW_DETAIL"
else
  echo "metric_value=50"
  echo "DENEB_METRIC_DETAIL build=ok server=ok mode=pairwise judge=skipped"
fi
PAIRWISE_EOF

  chmod +x "$script"
  echo "$script"
}

gen_bench() {
  local script="$OUT_DIR/deneb-metric-bench.sh"
  _write_header "$script"
  _write_server_start "$script"

  cat >> "$script" << 'BENCH_EOF'

# Full benchmark metric: runs bench-challenge + bench-multiturn + bench-oolong tests.
# Returns pass rate as metric (0-100).
BENCH_OUT=$(python3 "$REPO_DIR/scripts/dev-quality-test.py" \
  --scenario bench --json --port "$PORT" 2>&1) || true

# Extract overall score from JSON output.
SCORE=$(echo "$BENCH_OUT" | python3 -c "
import sys, json
try:
    data = json.loads(sys.stdin.read())
    print(round(data.get('overall_score', 0) * 100))
except:
    print(0)
" 2>/dev/null || echo "0")

PASSED=$(echo "$BENCH_OUT" | python3 -c "
import sys, json
try:
    data = json.loads(sys.stdin.read())
    print(f\"{data.get('passed_tests', 0)}/{data.get('total_tests', 0)}\")
except:
    print('0/0')
" 2>/dev/null || echo "0/0")

echo "metric_value=$SCORE"
echo "DENEB_METRIC_DETAIL build=ok server=ok bench_score=$SCORE tests=$PASSED"
BENCH_EOF

  chmod +x "$script"
  echo "$script"
}

cmd_list() {
  echo "Available metric presets:"
  echo ""
  echo "  smoke      Health + Ready (metric_value=0~2, ~2s)"
  echo "  quality    Chat quality score (metric_value=0~100, ~30s)"
  echo "  combined   Smoke(20%) + Quality(80%) (metric_value=0~100, ~30s)"
  echo "  custom     Custom message quality (metric_value=0~100, ~30s)"
  echo "             Usage: dev-metric-gen.sh custom \"메시지\""
  echo ""
  echo "  --- Benchmark Presets ---"
  echo "  judge      LLM-as-Judge quality (heuristic 30% + LLM judge 70%, 0~100)"
  echo "             Requires JUDGE_API_KEY or ANTHROPIC_API_KEY"
  echo "  pairwise   Pairwise A/B comparison (baseline vs candidate, 0/50/100)"
  echo "             Requires JUDGE_API_KEY + production gateway on port 18789"
  echo "  bench      Full benchmark suite (Arena-Hard + MT-Bench + Oolong, 0~100)"
  echo "             Runs 23 benchmark tests via Telegram"
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
  judge)    gen_judge ;;
  pairwise) gen_pairwise ;;
  bench)    gen_bench ;;
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
