#!/usr/bin/env bash
# Install the daily Health+RSI bench snapshot refresh timer on the production host.
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-bench-refresh.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-bench-refresh must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR"
chmod +x "$REPO_DIR/scripts/audit/refresh-bench-snapshots.sh"
chmod +x "$REPO_DIR/scripts/audit/bench-ratchet-notify.sh"

install -m 0644 "$SCRIPT_DIR/deneb-bench-refresh.service" "$USER_SYSTEMD_DIR/deneb-bench-refresh.service"
install -m 0644 "$SCRIPT_DIR/deneb-bench-refresh.timer" "$USER_SYSTEMD_DIR/deneb-bench-refresh.timer"
# OnFailure= target: carries a ratchet breach out to the operator's issue
# tracker. Without it the refresh's deliberate exit 1 only reaches whoever runs
# `systemctl --user status` — which, for 12 straight red days, was nobody.
install -m 0644 "$SCRIPT_DIR/deneb-bench-refresh-notify.service" "$USER_SYSTEMD_DIR/deneb-bench-refresh-notify.service"

systemctl --user daemon-reload
systemctl --user enable --now deneb-bench-refresh.timer

echo "Deneb bench-refresh timer installed."
echo "Useful commands:"
echo "  systemctl --user list-timers deneb-bench-refresh.timer"
echo "  systemctl --user start deneb-bench-refresh.service"
echo "  journalctl --user -u deneb-bench-refresh.service -n 50"
echo "  scripts/audit/bench-ratchet-notify.sh breach   # dry-run the operator report"
