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
# The runtime cache now lands in ~/.deneb/data (health_v3/runtime.py
# state_cache_path) — the tracked seed under scripts/audit/ is never written,
# so the production tree stays clean by construction. The old revert here
# (git checkout -- the tracked cache) is exactly what kept the on-disk cache
# permanently stale: every fast consumer read the last COMMITTED copy, blew the
# 72h TTL, and the health-finding miner fell back to the v2 bench for nine days
# (2026-07-18 → 07-27) without anyone noticing.
python3 scripts/audit/health-bench-v3.py --deep --refresh-runtime-cache \
  --write-snapshot --append-history

# 2) RSI deep refresh embeds the fresh health snapshot into the state-dir cache (~/.deneb/data/rsi-bench-cache.json)
#    and writes rsi-bench-snapshot.json for meta RSI evidence.
python3 scripts/audit/rsi-bench.py --deep --refresh-cache \
  --write-snapshot --append-history

# 3) e-process cutover evidence. The ladder's rollback half is structurally
#    near-unreachable by waiting (no evolve has ever regressed), and this
#    backtest is the designed substitute: it replays the archived usage history
#    through BOTH rollback deciders and reports their agreement. It existed as a
#    manual CLI wired nowhere, so the evidence the cutover decision needs was
#    never actually produced. Read-only over ~/.deneb/data.
if [[ -d gateway-go/cmd/rsi-backtest ]]; then
  out="$HOME/.deneb/data/rsi-backtest-latest.txt"
  if (cd gateway-go && go run ./cmd/rsi-backtest) > "$out.tmp" 2>&1; then
    mv "$out.tmp" "$out"
    echo "e-process backtest written to $out"
    head -2 "$out"
  else
    rm -f "$out.tmp"
    echo "WARNING: rsi-backtest failed; leaving the previous snapshot in place" >&2
  fi
fi

echo "bench snapshots refreshed under $ROOT/scripts/audit/"
