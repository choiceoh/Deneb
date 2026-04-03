#!/bin/bash
# bench-search-quality.sh — Autoresearch metric_cmd for memory search quality.
# Runs TestSearchBenchmarkMRR and outputs MRR@10 metric.
# Usage: bash scripts/bench-search-quality.sh
set -uo pipefail
cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
output=$(go test -v -run TestSearchBenchmarkMRR -count=1 \
  -tags no_ffi ./gateway-go/internal/memory/ 2>&1) || true
# Show last 30 lines for debugging context.
echo "$output" | tail -30
# Ensure METRIC line is present. If test failed, output 0 so
# extractMetricSmart doesn't pick up a stray number.
if ! echo "$output" | grep -q '^METRIC:'; then
  echo "METRIC: 0"
fi
