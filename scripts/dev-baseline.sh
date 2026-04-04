#!/usr/bin/env bash
# Baseline tracking for dev iteration metrics.
#
# Saves and compares test results against a per-branch baseline.
# Used by AI agents to detect quality/performance regressions.
#
# Usage:
#   scripts/dev-baseline.sh save       # save current result as baseline
#   scripts/dev-baseline.sh compare    # compare current result vs baseline
#   scripts/dev-baseline.sh show       # show saved baseline for current branch
#   scripts/dev-baseline.sh history    # list all saved baselines
#
# Output (compare):
#   BASELINE_COMPARE metric=85→90(+5) korean=20→25(+5) latency=2100→1800(-300ms)
#   REGRESSION: (none)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BASELINE_DIR="/tmp/deneb-baselines"
RESULT_FILE="/tmp/deneb-iterate-result.json"

mkdir -p "$BASELINE_DIR"

_current_branch() {
  git -C "$REPO_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null | tr '/' '_' || echo "unknown"
}

_current_commit() {
  git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown"
}

_baseline_file() {
  echo "$BASELINE_DIR/$(_current_branch).json"
}

cmd_save() {
  if [[ ! -f "$RESULT_FILE" ]]; then
    echo "ERROR: no result file found at $RESULT_FILE"
    echo "  Run scripts/dev-iterate.sh first to generate a result."
    exit 1
  fi

  local branch commit timestamp baseline_file
  branch=$(_current_branch)
  commit=$(_current_commit)
  timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  baseline_file=$(_baseline_file)

  # Add metadata to the saved baseline.
  python3 -c "
import json, sys
with open('$RESULT_FILE') as f:
    data = json.load(f)
data['baseline_meta'] = {
    'branch': '$branch',
    'commit': '$commit',
    'saved_at': '$timestamp',
}
with open('$baseline_file', 'w') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
print(f'saved: $baseline_file (branch=$branch commit=$commit)')
"
}

cmd_compare() {
  local baseline_file
  baseline_file=$(_baseline_file)

  if [[ ! -f "$baseline_file" ]]; then
    echo "NO_BASELINE: no baseline saved for branch $(_current_branch)"
    echo "  Run: scripts/dev-baseline.sh save"
    exit 0
  fi

  if [[ ! -f "$RESULT_FILE" ]]; then
    echo "ERROR: no current result at $RESULT_FILE"
    echo "  Run scripts/dev-iterate.sh first."
    exit 1
  fi

  python3 -c "
import json, sys

with open('$baseline_file') as f:
    base = json.load(f)
with open('$RESULT_FILE') as f:
    curr = json.load(f)

# Extract comparable metrics.
def get_metric(d):
    checks = d.get('checks', [])
    passed = sum(1 for c in checks if c.get('ok', False))
    return passed

def get_quality(d):
    return d.get('quality', {})

def get_latency(d):
    phases = d.get('phase', {})
    return sum(p.get('ms', 0) for p in phases.values())

base_metric = get_metric(base)
curr_metric = get_metric(curr)
base_q = get_quality(base)
curr_q = get_quality(curr)
base_lat = get_latency(base)
curr_lat = get_latency(curr)

# Build comparison string.
parts = []
metric_delta = curr_metric - base_metric
parts.append(f'metric={base_metric}→{curr_metric}({metric_delta:+d})')

latency_delta = curr_lat - base_lat
parts.append(f'latency={base_lat}→{curr_lat}({latency_delta:+d}ms)')

# Compare quality components.
regressions = []
for key in sorted(set(list(base_q.keys()) + list(curr_q.keys()))):
    bv = base_q.get(key)
    cv = curr_q.get(key)
    if bv is None or cv is None:
        continue
    if isinstance(bv, bool) or isinstance(cv, bool):
        if bv and not cv:
            regressions.append(f'{key}: true→false')
        parts.append(f'{key}={bv}→{cv}')
    elif isinstance(bv, (int, float)) and isinstance(cv, (int, float)):
        delta = cv - bv
        parts.append(f'{key}={bv}→{cv}({delta:+g})')
        if delta < 0 and abs(delta) >= 5:
            regressions.append(f'{key}={delta:+g}')

# Check for metric regression.
if metric_delta < 0:
    regressions.insert(0, f'metric={metric_delta:+d}')

# Check for latency regression (>20% increase).
if base_lat > 0 and latency_delta > 0 and (latency_delta / base_lat) > 0.2:
    regressions.append(f'latency={latency_delta:+d}ms (+{latency_delta/base_lat:.0%})')

base_meta = base.get('baseline_meta', {})
print(f\"\"\"BASELINE_COMPARE {' '.join(parts)}
  baseline: {base_meta.get('commit', '?')} ({base_meta.get('saved_at', '?')})\"\"\")

if regressions:
    print(f'REGRESSION: {\", \".join(regressions)}')
    sys.exit(1)
else:
    print('REGRESSION: (none)')
    sys.exit(0)
"
}

cmd_show() {
  local baseline_file
  baseline_file=$(_baseline_file)

  if [[ ! -f "$baseline_file" ]]; then
    echo "no baseline for branch $(_current_branch)"
    exit 0
  fi

  python3 -c "
import json
with open('$baseline_file') as f:
    data = json.load(f)
meta = data.get('baseline_meta', {})
checks = data.get('checks', [])
quality = data.get('quality', {})
phases = data.get('phase', {})

passed = sum(1 for c in checks if c.get('ok', False))
total = len(checks)
latency = sum(p.get('ms', 0) for p in phases.values())

print(f'branch:  {meta.get(\"branch\", \"?\")}')
print(f'commit:  {meta.get(\"commit\", \"?\")}')
print(f'saved:   {meta.get(\"saved_at\", \"?\")}')
print(f'checks:  {passed}/{total}')
print(f'latency: {latency}ms')
if quality:
    print(f'quality: {json.dumps(quality)}')
"
}

cmd_history() {
  echo "saved baselines:"
  for f in "$BASELINE_DIR"/*.json; do
    [[ -f "$f" ]] || continue
    branch=$(basename "$f" .json)
    meta=$(python3 -c "
import json
with open('$f') as fp:
    d = json.load(fp)
m = d.get('baseline_meta', {})
print(f\"{m.get('commit', '?')} {m.get('saved_at', '?')}\")
" 2>/dev/null || echo "?")
    echo "  $branch: $meta"
  done
}

# --- Main ---
case "${1:-help}" in
  save)    cmd_save ;;
  compare) cmd_compare ;;
  show)    cmd_show ;;
  history) cmd_history ;;
  *)
    echo "Usage: dev-baseline.sh {save|compare|show|history}"
    exit 1
    ;;
esac
