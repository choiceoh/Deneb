#!/usr/bin/env bash
# Refresh Health Bench 3 + RSI Bench snapshots on the production checkout.
#
# Writes gitignored artifacts only (snapshots / history). Restores the checked-in
# health-v3-runtime-cache.json if deep refresh dirties it, so auto-deploy does
# not see a tracked dirty tree.
#
# Usage (production host, ~/deneb on main):
#   scripts/audit/refresh-bench-snapshots.sh
#   make bench-refresh
set -euo pipefail

ROOT="${DENEB_PROD_DIR:-$HOME/deneb}"
cd "$ROOT"

if [[ ! -f scripts/audit/health-bench-v3.py || ! -f scripts/audit/rsi-bench.py ]]; then
  echo "ERROR: expected audit CLIs under $ROOT/scripts/audit" >&2
  exit 1
fi

# 1) Force-overwrite health-v3 snapshot (meta QualityBench + RSI codebase-delta).
python3 scripts/audit/health-bench-v3.py --deep --refresh-runtime-cache \
  --write-snapshot --append-history

# Keep production tree clean for auto-deploy (runtime-cache is checked in).
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git checkout -- scripts/audit/health-v3-runtime-cache.json 2>/dev/null || true
fi

# 2) RSI deep refresh embeds the fresh health snapshot into the state-dir cache (~/.deneb/data/rsi-bench-cache.json)
#    and writes rsi-bench-snapshot.json for meta RSI evidence.
python3 scripts/audit/rsi-bench.py --deep --refresh-cache \
  --write-snapshot --append-history

echo "bench snapshots refreshed under $ROOT/scripts/audit/"
