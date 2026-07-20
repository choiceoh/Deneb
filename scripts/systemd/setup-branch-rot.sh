#!/usr/bin/env bash
# Install the branch-rot miner timer on the production host.
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-branch-rot.sh
#
# The timer runs scripts/audit/branch-rot-miner.py weekly: worktrunk's fleet
# snapshot (wt list --format json) of the dev checkout becomes propose-only
# scope=code recovery candidates for stale ahead-of-main branches. Idempotent:
# re-running just refreshes the installed units from the repo copies.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-branch-rot must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR"
chmod +x "$REPO_DIR/scripts/audit/branch-rot-miner.py"

install -m 0644 "$SCRIPT_DIR/deneb-branch-rot.service" "$USER_SYSTEMD_DIR/deneb-branch-rot.service"
install -m 0644 "$SCRIPT_DIR/deneb-branch-rot.timer" "$USER_SYSTEMD_DIR/deneb-branch-rot.timer"

systemctl --user daemon-reload
systemctl --user enable --now deneb-branch-rot.timer

echo "Deneb branch-rot timer installed."
