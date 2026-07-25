#!/usr/bin/env bash
# Install the proactive L4 supply miner timers on the production host.
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-l4-miners.sh
#
# RSI roadmap P5 workstream 3 ("proactive L4 supply — self-renovation, not just
# self-repair") shipped four miners, but only branch-rot ever got a schedule:
# the other four were workflow_dispatch-only, so the lane that exists to keep L4
# renovating when NOTHING is broken ran only when a human remembered to click it.
# Live evidence at install time (2026-07-25): tool-quality and sop had produced
# zero candidates since landing (07-14 / 07-17) while their dry-runs showed
# signal waiting; deadcode's last candidate was 9.6 days old.
#
# Each miner is propose-only, per-run capped, and dedups against the tracker over
# the miniapp RPC — cadence here sets throughput, not safety. Idempotent:
# re-running just refreshes the installed units from the repo copies.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

# Unit name <-> script name are the same stem: deneb-<m>.{service,timer} runs
# scripts/audit/<m>-miner.py.
MINERS=(health-finding deadcode-finding tool-quality sop)

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-l4-miners must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR"

for miner in "${MINERS[@]}"; do
  script="$REPO_DIR/scripts/audit/${miner}-miner.py"
  [[ -f "$script" ]] || { echo "ERROR: missing $script" >&2; exit 1; }
  chmod +x "$script"

  install -m 0644 "$SCRIPT_DIR/deneb-${miner}.service" "$USER_SYSTEMD_DIR/deneb-${miner}.service"
  install -m 0644 "$SCRIPT_DIR/deneb-${miner}.timer" "$USER_SYSTEMD_DIR/deneb-${miner}.timer"
done

systemctl --user daemon-reload

timers=()
for miner in "${MINERS[@]}"; do
  timers+=("deneb-${miner}.timer")
done

systemctl --user enable --now "${timers[@]}"

echo "Deneb L4 supply miner timers installed:"
systemctl --user list-timers --all "${timers[@]}"
