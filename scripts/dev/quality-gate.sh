#!/usr/bin/env bash
# Korean-response quality regression gate for `make check`.
#
# The Go unit tests stay green even when Korean *output* quality silently
# regresses (tone, leak-free formatting, substance) — that failure mode only
# shows up end-to-end against a live gateway + model, which a human normally has
# to run by hand (scripts/dev/live-test.sh quality). This wraps that live check
# as an automatic build gate.
#
# Safety: it is OPT-IN. With DENEB_QUALITY_GATE unset (the default everywhere
# except a configured DGX Spark gateway) it skips cleanly and exits 0, so adding
# it to `make check` never breaks the build on a machine without a live model.
# Set DENEB_QUALITY_GATE=1 on the DGX host (where a gateway is running) to arm it.
#
# Flow when armed:
#   iterate.sh quality  ->  $TMP/...-iterate-result.json  ->  baseline.sh compare
# baseline.sh compare already exits non-zero on a per-branch regression, so this
# script just gates on the environment and propagates that exit code.
#
# First run on a branch has no baseline: baseline.sh prints NO_BASELINE and
# exits 0 (not a regression). Save one with: scripts/dev/baseline.sh save
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ "${DENEB_QUALITY_GATE:-}" != "1" ]]; then
  echo "quality-gate: skipped (set DENEB_QUALITY_GATE=1 on a live DGX gateway to enable the Korean-quality regression gate)"
  exit 0
fi

echo "quality-gate: armed — running live Korean-quality regression check"

# Produce the current result file (iterate.sh writes the quality JSON that
# baseline.sh compares). A live gateway must be reachable; if the run fails the
# gate fails, which is the intended behavior when explicitly armed.
"$SCRIPT_DIR/iterate.sh" quality

# Compare against this branch's saved baseline. Exits 1 on regression.
"$SCRIPT_DIR/baseline.sh" compare
