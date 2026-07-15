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

install -m 0644 "$SCRIPT_DIR/deneb-bench-refresh.service" "$USER_SYSTEMD_DIR/deneb-bench-refresh.service"
install -m 0644 "$SCRIPT_DIR/deneb-bench-refresh.timer" "$USER_SYSTEMD_DIR/deneb-bench-refresh.timer"

systemctl --user daemon-reload
systemctl --user enable --now deneb-bench-refresh.timer

echo "Deneb bench-refresh timer installed."
echo "Useful commands:"
echo "  systemctl --user list-timers deneb-bench-refresh.timer"
echo "  systemctl --user start deneb-bench-refresh.service"
echo "  journalctl --user -u deneb-bench-refresh.service -n 50"
