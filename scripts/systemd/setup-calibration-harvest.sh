#!/usr/bin/env bash
# Install the dated one-shot that closes the RSI P5-2 calibration window on
# 2026-08-23: harvest the campaign's bench evidence into a report (+ wiki),
# remove the rsi-calibration.conf drop-in, and reload the gateway.
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-calibration-harvest.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-calibration-harvest must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR"

install -m 0644 "$SCRIPT_DIR/deneb-calibration-harvest.service" "$USER_SYSTEMD_DIR/deneb-calibration-harvest.service"
install -m 0644 "$SCRIPT_DIR/deneb-calibration-harvest.timer" "$USER_SYSTEMD_DIR/deneb-calibration-harvest.timer"

systemctl --user daemon-reload
systemctl --user enable --now deneb-calibration-harvest.timer

echo "Deneb calibration-harvest timer installed (fires 2026-08-23 09:00)."
echo "Useful commands:"
echo "  systemctl --user list-timers deneb-calibration-harvest.timer"
echo "  python3 scripts/audit/calibration_harvest.py            # mid-window readout (no revert)"
echo "  journalctl --user -u deneb-calibration-harvest.service -n 50"
