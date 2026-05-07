#!/usr/bin/env bash
# Install the pull-based Deneb auto-deploy timer for the production host.
#
# Usage:
#   scripts/systemd/setup-auto-deploy.sh
#
# The timer checks origin/main on a short interval. When a merged PR changes
# main, scripts/deploy/auto-deploy.sh builds the gateway and asks the
# systemd-managed gateway to restart with SIGUSR1 after the build succeeds.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"
STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
GATEWAY_DROPIN_DIR="$USER_SYSTEMD_DIR/deneb-gateway.service.d"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-auto-deploy must be run from the production main checkout." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR" "$STATE_DIR" "$GATEWAY_DROPIN_DIR"

install -m 0644 "$SCRIPT_DIR/deneb-auto-deploy.service" "$USER_SYSTEMD_DIR/deneb-auto-deploy.service"
install -m 0644 "$SCRIPT_DIR/deneb-auto-deploy.timer" "$USER_SYSTEMD_DIR/deneb-auto-deploy.timer"

# SIGUSR1 is Deneb's graceful restart signal. The gateway exits with 75 so a
# systemd Restart= policy can bring it back without flagging a failed service.
cat > "$GATEWAY_DROPIN_DIR/restart-exit-status.conf" <<'EOF'
[Service]
SuccessExitStatus=0 75 143
EOF

git rev-parse HEAD > "$STATE_DIR/auto-deploy.deployed-head"

systemctl --user daemon-reload
systemctl --user enable --now deneb-auto-deploy.timer

echo "Deneb auto-deploy timer installed."
echo "Useful commands:"
echo "  systemctl --user list-timers deneb-auto-deploy.timer"
echo "  systemctl --user start deneb-auto-deploy.service"
echo "  tail -f /tmp/deneb-auto-deploy.log"
