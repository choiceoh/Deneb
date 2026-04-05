#!/usr/bin/env bash
# Compaction benchmark metric for autoresearch with live calibration.
#
# Fast path (~2s):  Go benchmark only, applies calibration factor.
# Calibration (every 10th iteration, ~32s): benchmark + live test,
#   updates calibration factor via EMA.
#
# Output: metric_value=N (0-100)
#
# Env vars:
#   AUTORESEARCH_ITERATION — current iteration (0=baseline)
#   CALIBRATION_INTERVAL   — iterations between live calibration (default: 10)
#   CALIBRATION_FILE       — path to persist calibration state
#   METRIC_PORT            — port for live test gateway (default: 18791)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="${AUTORESEARCH_REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"

ITERATION="${AUTORESEARCH_ITERATION:-0}"
CAL_INTERVAL="${CALIBRATION_INTERVAL:-10}"
CAL_FILE="${CALIBRATION_FILE:-/tmp/deneb-compaction-calibration.json}"
PORT="${METRIC_PORT:-18791}"

# ── Calibration state management ──────────────────────────────────────────────

read_factor() {
  if [[ -f "$CAL_FILE" ]]; then
    python3 -c "
import json, sys
try:
    d = json.load(open('$CAL_FILE'))
    print(d.get('factor', 1.0))
except:
    print(1.0)
" 2>/dev/null || echo "1.0"
  else
    echo "1.0"
  fi
}

update_calibration() {
  local bench_score="$1"
  local live_score="$2"
  python3 -c "
import json, os

cal_file = '$CAL_FILE'
bench = float('$bench_score')
live = float('$live_score')

# Load existing state.
state = {'factor': 1.0, 'history': []}
if os.path.exists(cal_file):
    try:
        state = json.load(open(cal_file))
    except:
        pass

# Compute new ratio and EMA.
old_factor = state.get('factor', 1.0)
if bench > 0.01:
    new_ratio = live / bench
else:
    new_ratio = 1.0

# EMA with alpha=0.3 (recent live results weigh more).
alpha = 0.3
factor = (1 - alpha) * old_factor + alpha * new_ratio

# Clamp to [0.5, 1.5] safety range.
factor = max(0.5, min(1.5, factor))

# Save history entry.
state['factor'] = round(factor, 4)
state['history'] = (state.get('history', []) + [
    {'iteration': int('$ITERATION'), 'bench': round(bench, 2),
     'live': round(live, 2), 'ratio': round(new_ratio, 4),
     'factor': round(factor, 4)}
])[-20:]  # keep last 20

with open(cal_file, 'w') as f:
    json.dump(state, f, indent=2)

print(round(factor, 4))
" 2>/dev/null || echo "1.0"
}

# ── Benchmark runner ──────────────────────────────────────────────────────────

run_bench() {
  local score
  score=$(cd "$REPO_DIR/gateway-go" && go run ./cmd/compaction-metric 2>/dev/null | grep -oP 'metric_value=\K[\d.]+')
  if [[ -z "$score" ]]; then
    echo "0"
    return
  fi
  echo "$score"
}

# ── Live test runner ──────────────────────────────────────────────────────────

run_live() {
  local live_score="0"

  # Build and start dev gateway.
  if ! "$SCRIPT_DIR/dev-live-test.sh" restart >/dev/null 2>&1; then
    echo "0"
    return
  fi

  # Run quality metric.
  live_score=$("$SCRIPT_DIR/dev-quality-metric.sh" "$PORT" "이전 대화 내용을 기억하고 있나요? 아까 논의한 주제를 요약해 주세요." 2>/dev/null | grep -oP 'metric_value=\K[\d.]+' || echo "0")

  # Cleanup.
  "$SCRIPT_DIR/dev-live-test.sh" stop >/dev/null 2>&1 || true

  echo "$live_score"
}

# ── Main ──────────────────────────────────────────────────────────────────────

# Always run benchmark.
BENCH_SCORE=$(run_bench)

FACTOR=$(read_factor)

# Check if this is a calibration iteration.
if (( ITERATION > 0 )) && (( ITERATION % CAL_INTERVAL == 0 )); then
  # Calibration: also run live test.
  LIVE_SCORE=$(run_live)
  FACTOR=$(update_calibration "$BENCH_SCORE" "$LIVE_SCORE")

  >&2 echo "CALIBRATION: bench=$BENCH_SCORE live=$LIVE_SCORE factor=$FACTOR"
fi

# Apply calibration factor.
FINAL=$(python3 -c "
bench = float('$BENCH_SCORE')
factor = float('$FACTOR')
final = max(0, min(100, bench * factor))
print(f'{final:.4f}')
" 2>/dev/null || echo "$BENCH_SCORE")

echo "metric_value=$FINAL"
echo "DENEB_METRIC_DETAIL bench=$BENCH_SCORE factor=$FACTOR iteration=$ITERATION"
