#!/usr/bin/env bash
# Autoresearch results parser for AI agents.
#
# Reads .autoresearch/config.json and results.tsv directly (no gateway needed),
# outputs structured data for AI agents to interpret.
#
# Usage:
#   scripts/dev-ar-results.sh                    # human-readable summary
#   scripts/dev-ar-results.sh --json             # structured JSON
#   scripts/dev-ar-results.sh --table            # compact table
#   scripts/dev-ar-results.sh --best             # top improvements only
#   scripts/dev-ar-results.sh --failures         # recent failures analysis
#   scripts/dev-ar-results.sh --suggest          # next action suggestion
#
# Output:
#   --json mode emits: DENEB_AR_RESULTS {...}

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Autoresearch data directory.
AR_DIR="$REPO_DIR/.autoresearch"
CONFIG_FILE="$AR_DIR/config.json"
RESULTS_FILE="$AR_DIR/results.tsv"

FORMAT="${1:-summary}"

_check_exists() {
  if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "NO_EXPERIMENT: no autoresearch config found at $CONFIG_FILE"
    echo "  Start with: scripts/dev-autoresearch.sh start --target FILE"
    exit 0
  fi
}

cmd_json() {
  _check_exists
  python3 -c "
import json, sys, os

config_path = '$CONFIG_FILE'
results_path = '$RESULTS_FILE'

with open(config_path) as f:
    cfg = json.load(f)

# Parse results TSV.
rows = []
if os.path.exists(results_path):
    with open(results_path) as f:
        lines = f.read().strip().split('\n')
    if len(lines) > 1:
        for line in lines[1:]:
            fields = line.split('\t')
            if len(fields) < 7:
                continue
            rows.append({
                'iteration': int(fields[0]) if fields[0].isdigit() else 0,
                'hypothesis': fields[2] if len(fields) > 2 else '',
                'metric': float(fields[3]) if len(fields) > 3 else 0,
                'kept': fields[4] == 'true' if len(fields) > 4 else False,
                'duration_sec': int(fields[6]) if len(fields) > 6 and fields[6].isdigit() else 0,
                'best_so_far': float(fields[7]) if len(fields) > 7 else 0,
                'delta': float(fields[8]) if len(fields) > 8 else 0,
            })

# Compute statistics.
total = cfg.get('total_iterations', len(rows))
kept = cfg.get('kept_iterations', sum(1 for r in rows if r['kept']))
baseline = cfg.get('baseline_metric')
best = cfg.get('best_metric')
consec_fail = cfg.get('consecutive_failures', 0)

success_rate = kept / total if total > 0 else 0
improvement_pct = 0
if baseline is not None and best is not None and baseline != 0:
    if cfg.get('metric_direction') == 'maximize':
        improvement_pct = (best - baseline) / abs(baseline) * 100
    else:
        improvement_pct = (baseline - best) / abs(baseline) * 100

# Top changes (kept, sorted by delta).
kept_rows = [r for r in rows if r['kept'] and r['iteration'] > 0]
kept_rows.sort(key=lambda r: abs(r['delta']), reverse=True)
top_changes = [{'iteration': r['iteration'], 'hypothesis': r['hypothesis'][:100],
                'delta': r['delta'], 'metric': r['metric']} for r in kept_rows[:5]]

# Recent failures.
recent_fail = []
for r in reversed(rows):
    if not r['kept'] and r['iteration'] > 0:
        recent_fail.append({'iteration': r['iteration'], 'hypothesis': r['hypothesis'][:100],
                           'metric': r['metric']})
    if len(recent_fail) >= 5:
        break

# Suggestion.
suggestion = 'continue'
if total == 0:
    suggestion = 'not_started'
elif consec_fail >= 8:
    suggestion = 'stop_and_review'
elif consec_fail >= 5:
    suggestion = 'change_strategy'
elif consec_fail >= 3:
    suggestion = 'try_different_approach'
elif total >= cfg.get('params', {}).get('max_iterations', 20):
    suggestion = 'completed'
elif success_rate > 0.5 and total > 5:
    suggestion = 'continue_exploitation'
elif success_rate < 0.2 and total > 5:
    suggestion = 'reconsider_metric_or_targets'
else:
    suggestion = 'continue_exploration'

result = {
    'status': 'running' if os.path.exists('$AR_DIR/runner.lock') else 'stopped',
    'metric_name': cfg.get('metric_name', ''),
    'direction': cfg.get('metric_direction', ''),
    'baseline': baseline,
    'best': best,
    'improvement_pct': round(improvement_pct, 1),
    'total_iterations': total,
    'kept': kept,
    'success_rate': round(success_rate, 3),
    'consecutive_failures': consec_fail,
    'target_files': cfg.get('target_files', []),
    'branch_tag': cfg.get('branch_tag', ''),
    'top_changes': top_changes,
    'recent_failures': recent_fail,
    'suggestion': suggestion,
}
print('DENEB_AR_RESULTS ' + json.dumps(result, ensure_ascii=False))
"
}

cmd_table() {
  _check_exists
  if [[ ! -f "$RESULTS_FILE" ]]; then
    echo "NO_RESULTS: no results.tsv found"
    exit 0
  fi
  python3 -c "
import os

with open('$RESULTS_FILE') as f:
    lines = f.read().strip().split('\n')

if len(lines) <= 1:
    print('(no iterations yet)')
    exit()

print(f'{'#':>3} | {'metric':>10} | {'kept':>5} | {'delta':>10} | {'dur':>4} | hypothesis')
print('-' * 80)
for line in lines[1:]:
    fields = line.split('\t')
    if len(fields) < 7:
        continue
    it = fields[0]
    metric = fields[3] if len(fields) > 3 else ''
    kept = 'KEEP' if (len(fields) > 4 and fields[4] == 'true') else 'drop'
    delta = fields[8] if len(fields) > 8 else ''
    dur = fields[6] if len(fields) > 6 else ''
    hyp = (fields[2][:50] + '..') if len(fields[2]) > 50 else fields[2]
    print(f'{it:>3} | {metric:>10} | {kept:>5} | {delta:>10} | {dur:>4} | {hyp}')
"
}

cmd_best() {
  _check_exists
  if [[ ! -f "$RESULTS_FILE" ]]; then
    echo "NO_RESULTS"
    exit 0
  fi
  python3 -c "
with open('$RESULTS_FILE') as f:
    lines = f.read().strip().split('\n')

if len(lines) <= 1:
    print('(no iterations yet)')
    exit()

kept = []
for line in lines[1:]:
    fields = line.split('\t')
    if len(fields) > 4 and fields[4] == 'true' and fields[0] != '0':
        delta = float(fields[8]) if len(fields) > 8 else 0
        kept.append((abs(delta), fields))

kept.sort(reverse=True)
print('Top improvements (kept iterations):')
for _, fields in kept[:10]:
    it, hyp, metric = fields[0], fields[2], fields[3]
    delta = fields[8] if len(fields) > 8 else '0'
    print(f'  #{it}: metric={metric} delta={delta}')
    print(f'        {hyp[:80]}')
"
}

cmd_failures() {
  _check_exists
  if [[ ! -f "$RESULTS_FILE" ]]; then
    echo "NO_RESULTS"
    exit 0
  fi
  python3 -c "
with open('$RESULTS_FILE') as f:
    lines = f.read().strip().split('\n')

if len(lines) <= 1:
    print('(no iterations yet)')
    exit()

failures = []
for line in lines[1:]:
    fields = line.split('\t')
    if len(fields) > 4 and fields[4] != 'true' and fields[0] != '0':
        failures.append(fields)

if not failures:
    print('No failed iterations.')
    exit()

print(f'Failed iterations: {len(failures)}')
print()

# Recent failures.
print('Recent failures:')
for fields in failures[-5:]:
    it, hyp, metric = fields[0], fields[2], fields[3]
    print(f'  #{it}: metric={metric}')
    print(f'        {hyp[:80]}')

# Pattern analysis: group by first word of hypothesis.
from collections import Counter
prefixes = Counter()
for fields in failures:
    hyp = fields[2].lower().strip()
    first_word = hyp.split()[0] if hyp.split() else '?'
    prefixes[first_word] += 1

print()
print('Failure patterns (by hypothesis prefix):')
for prefix, count in prefixes.most_common(5):
    print(f'  {prefix}: {count} failures')
"
}

cmd_suggest() {
  # Check if experiment exists first.
  if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "SUGGESTION: not_started"
    echo "  iterations: 0 (kept: 0)"
    echo "  consecutive_failures: 0"
    echo "  action: Start autoresearch with dev-autoresearch.sh start"
    return 0
  fi

  # Delegate to JSON mode and extract suggestion.
  local json_out
  json_out=$(cmd_json 2>/dev/null)
  local suggestion
  suggestion=$(echo "$json_out" | grep '^DENEB_AR_RESULTS ' | sed 's/^DENEB_AR_RESULTS //' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('suggestion','unknown'))" 2>/dev/null || echo "unknown")

  local total kept consec baseline best
  total=$(echo "$json_out" | grep '^DENEB_AR_RESULTS ' | sed 's/^DENEB_AR_RESULTS //' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('total_iterations',0))" 2>/dev/null || echo "0")
  kept=$(echo "$json_out" | grep '^DENEB_AR_RESULTS ' | sed 's/^DENEB_AR_RESULTS //' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('kept',0))" 2>/dev/null || echo "0")
  consec=$(echo "$json_out" | grep '^DENEB_AR_RESULTS ' | sed 's/^DENEB_AR_RESULTS //' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('consecutive_failures',0))" 2>/dev/null || echo "0")

  echo "SUGGESTION: $suggestion"
  echo "  iterations: $total (kept: $kept)"
  echo "  consecutive_failures: $consec"

  case "$suggestion" in
    not_started)
      echo "  action: Start autoresearch with dev-autoresearch.sh start" ;;
    stop_and_review)
      echo "  action: 8+ consecutive failures. Stop, review approach, possibly change target files or metric." ;;
    change_strategy)
      echo "  action: 5+ consecutive failures. Try a fundamentally different approach." ;;
    try_different_approach)
      echo "  action: 3+ consecutive failures. Pivot: change hyperparameters→structure or vice versa." ;;
    completed)
      echo "  action: Max iterations reached. Review results with --best, apply if satisfied." ;;
    continue_exploitation)
      echo "  action: Good success rate. Continue with fine-tuning changes." ;;
    reconsider_metric_or_targets)
      echo "  action: Low success rate. Consider changing metric or target files." ;;
    *)
      echo "  action: Continue exploring." ;;
  esac
}

cmd_summary() {
  _check_exists
  python3 -c "
import json, os

with open('$CONFIG_FILE') as f:
    cfg = json.load(f)

total = cfg.get('total_iterations', 0)
kept = cfg.get('kept_iterations', 0)
baseline = cfg.get('baseline_metric')
best = cfg.get('best_metric')
consec = cfg.get('consecutive_failures', 0)

print(f'=== Autoresearch: {cfg.get(\"metric_name\", \"?\")} ({cfg.get(\"metric_direction\", \"?\")}) ===')
print(f'Target: {\", \".join(cfg.get(\"target_files\", []))}')
print(f'Branch: autoresearch/{cfg.get(\"branch_tag\", \"?\")}')
print(f'Iterations: {total} (kept: {kept}, discarded: {total - kept})')
if total > 0:
    print(f'Success rate: {kept/total:.0%}')
if baseline is not None:
    print(f'Baseline: {baseline:.6f}')
if best is not None:
    print(f'Best: {best:.6f}')
    if baseline and baseline != 0:
        d = cfg.get('metric_direction', 'maximize')
        imp = (best - baseline) / abs(baseline) * 100 if d == 'maximize' else (baseline - best) / abs(baseline) * 100
        print(f'Improvement: {imp:.1f}%')
if consec > 0:
    print(f'Consecutive failures: {consec}')
"
}

# --- Main ---
case "$FORMAT" in
  --json)     cmd_json ;;
  --table)    cmd_table ;;
  --best)     cmd_best ;;
  --failures) cmd_failures ;;
  --suggest)  cmd_suggest ;;
  summary)    cmd_summary ;;
  *)
    echo "Usage: dev-ar-results.sh [--json|--table|--best|--failures|--suggest]"
    exit 1
    ;;
esac
