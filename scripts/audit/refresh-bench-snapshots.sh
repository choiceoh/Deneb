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
#    --check rides the SAME deep run (a second invocation would pay for the whole
#    report twice). Its exit code is captured rather than allowed to abort under
#    `set -e`: the refresh's own job is to produce snapshots, and it must finish
#    writing them even when the ratchet is breached. The breach is re-raised at
#    the end so the oneshot unit goes `failed` and stops being silent — RSI
#    Process sat below its baseline from 2026-07-25 to 08-08 with nothing wired
#    to notice, because the cadence only ever refreshed and never checked.
ratchet_check="$HOME/.deneb/data/rsi-bench-check-latest.txt"
mkdir -p "$(dirname "$ratchet_check")"
ratchet_breached=0
if python3 scripts/audit/rsi-bench.py --deep --refresh-cache \
  --write-snapshot --append-history --check > "$ratchet_check.tmp" 2>&1; then
  mv "$ratchet_check.tmp" "$ratchet_check"
else
  rc=$?
  mv "$ratchet_check.tmp" "$ratchet_check"
  # 2 = bench error (bad cache/baseline), 1 = ratchet breach. Both must be loud.
  if [[ $rc -eq 1 ]]; then
    ratchet_breached=1
    echo "WARNING: RSI Bench ratchet BREACHED — see $ratchet_check" >&2
  else
    echo "WARNING: RSI Bench run failed (exit $rc) — see $ratchet_check" >&2
    ratchet_breached=1
  fi
fi
cat "$ratchet_check"

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

# Green path: retire any open ratchet issue so the issue's open/closed state IS
# the live ratchet state (the breach half rides OnFailure= on the unit). The
# notifier is best-effort and always exits 0 — a notification must never turn a
# green refresh red.
if [[ $ratchet_breached -eq 0 ]]; then
  scripts/audit/bench-ratchet-notify.sh green || true
fi

if [[ $ratchet_breached -eq 1 ]]; then
  echo "ERROR: RSI Bench ratchet check did not pass — snapshots are written, but the" >&2
  echo "       run is reported as failed so the breach surfaces instead of decaying" >&2
  echo "       quietly. Details: $ratchet_check" >&2
  exit 1
fi
