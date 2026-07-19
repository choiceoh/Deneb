#!/usr/bin/env bash
# recall-metric.sh — recall quality score (0-100) for the iterate.sh loop.
#
# Runs the synthetic-corpus recall bench (recall_bench_test.go) and emits the
# metric_value line the optimization loop parses. Pure-Go, no GPU/LLM needed.
set -euo pipefail
cd "$(dirname "$0")/../../gateway-go"

out=$(go test ./internal/pipeline/chat/recall -run TestRecallQuality -count=1 -v 2>&1) && rc=0 || rc=$?
line=$(grep -o 'RECALL_METRIC hits=[0-9]* total=[0-9]* pct=[0-9]*' <<<"$out" | tail -1 || true)
if [[ $rc -ne 0 ]]; then
    echo "metric_value=0"
    echo "error: recall quality tests failed (go test exit $rc) — not reporting a partial metric" >&2
    grep -E '^\s*--- FAIL|^FAIL' <<<"$out" >&2 || true
    exit 1
fi
if [[ -z "$line" ]]; then
    echo "metric_value=0"
    echo "error: bench did not produce RECALL_METRIC (build failure?)" >&2
    exit 1
fi
pct=${line##*pct=}
echo "$line"
echo "metric_value=$pct"
