#!/bin/bash
# bench-search-quality.sh — Autoresearch metric_cmd for memory search quality.
# Runs TestSearchBenchmarkMRR and outputs MRR@10 metric.
# Usage: bash scripts/bench-search-quality.sh
set -euo pipefail
cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
go test -v -run TestSearchBenchmarkMRR -count=1 \
  -tags no_ffi ./gateway-go/internal/memory/ 2>&1 | tail -30
