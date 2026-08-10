#!/usr/bin/env bash
# Korean-response quality regression gate for `make check`.
#
# Unit tests and deterministic audits do not exercise the user-visible Korean
# reply path end to end. This wrapper keeps that check available from the normal
# local gate while remaining opt-in, because it needs a model-capable gateway
# environment and a branch baseline to be meaningful.
#
# Default behavior:
#   DENEB_QUALITY_GATE unset -> skip and exit 0.
#
# Armed behavior:
#   DENEB_QUALITY_GATE=1 scripts/dev/iterate.sh --metric quality
#   scripts/dev/baseline.sh compare
#
# The compare step owns the regression decision. First runs without a saved
# branch baseline print NO_BASELINE and pass; save a baseline explicitly with
# `scripts/dev/baseline.sh save` after reviewing a known-good quality result.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ "${DENEB_QUALITY_GATE:-}" != "1" ]]; then
  echo "quality-gate: skipped (set DENEB_QUALITY_GATE=1 to enable Korean-response quality regression checks)"
  exit 0
fi

echo "quality-gate: armed; running Korean-response quality regression check"

"$SCRIPT_DIR/iterate.sh" --metric quality
"$SCRIPT_DIR/baseline.sh" compare
