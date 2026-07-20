#!/usr/bin/env bash
# Longitudinal recall hit-rate scaffold (E1).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT/gateway-go"
go test ./internal/pipeline/chat/recall/ -run 'TestRecallLongitudinal_' -count=1 -v
