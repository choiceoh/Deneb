#!/usr/bin/env bash
# Install the RSI L4 coding dispatch timer on the production host.
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-coding-dispatch.sh
#
# The timer runs scripts/dev/coding-dispatch.sh every 2 hours, draining the
# self-correction queue into headless Codex runs. Idempotent: re-running just
# refreshes the installed units from the repo copies.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-coding-dispatch must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR"
chmod +x "$REPO_DIR/scripts/dev/coding-dispatch.sh"

install -m 0644 "$SCRIPT_DIR/deneb-coding-dispatch.service" "$USER_SYSTEMD_DIR/deneb-coding-dispatch.service"
install -m 0644 "$SCRIPT_DIR/deneb-coding-dispatch.timer" "$USER_SYSTEMD_DIR/deneb-coding-dispatch.timer"

systemctl --user daemon-reload
systemctl --user enable --now deneb-coding-dispatch.timer

echo "Deneb coding-dispatch timer installed."
echo "Useful commands:"
echo "  systemctl --user list-timers deneb-coding-dispatch.timer"
echo "  systemctl --user start deneb-coding-dispatch.service"
echo "  journalctl --user -u deneb-coding-dispatch.service -n 50"
