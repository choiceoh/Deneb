#!/usr/bin/env bash
# Setup the deneb gateway as a systemd user service with auto-start on boot.
#
# Usage:
#   scripts/systemd/setup-gateway-service.sh [--port PORT]
#
# This script:
#   1. Ensures deneb is built and the binary is linked into PATH
#   2. Enables systemd user linger (so user services survive logout)
#   3. Installs the gateway systemd user service via `deneb gateway install`
#   4. Verifies the service is running
#
# Prerequisites:
#   - systemd with user service support
#   - Node.js 22+
#   - pnpm (for building)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

PORT="${1:-}"
if [[ "$PORT" == "--port" ]]; then
  PORT="${2:-18789}"
elif [[ -z "$PORT" ]]; then
  PORT=""
fi

cd "$REPO_DIR"

# Step 1: Ensure the project is built
if [[ ! -d dist ]]; then
  echo "Building deneb..."
  pnpm build
fi

# Step 2: Link deneb into PATH if not already available
if ! command -v deneb &>/dev/null; then
  echo "Linking deneb into PATH..."
  npm link 2>/dev/null || {
    # Fallback: create symlink in a user-writable bin directory
    mkdir -p "$HOME/.local/bin"
    ln -sf "$REPO_DIR/deneb.mjs" "$HOME/.local/bin/deneb"
    chmod +x "$REPO_DIR/deneb.mjs"
    if ! echo "$PATH" | grep -q "$HOME/.local/bin"; then
      echo "Add ~/.local/bin to your PATH:"
      echo '  export PATH="$HOME/.local/bin:$PATH"'
      echo "Add this to your ~/.bashrc or ~/.profile for persistence."
      export PATH="$HOME/.local/bin:$PATH"
    fi
  }
fi

# Step 3: Enable systemd user linger (allows user services to run without an active login session)
if command -v loginctl &>/dev/null; then
  LINGER_STATUS=$(loginctl show-user "$(whoami)" --property=Linger 2>/dev/null | cut -d= -f2 || echo "unknown")
  if [[ "$LINGER_STATUS" != "yes" ]]; then
    echo "Enabling systemd user linger for $(whoami)..."
    loginctl enable-linger "$(whoami)" 2>/dev/null || sudo loginctl enable-linger "$(whoami)"
  fi
fi

# Step 4: Install the gateway service
echo "Installing deneb gateway service..."
INSTALL_ARGS=(gateway install --force)
if [[ -n "$PORT" ]]; then
  INSTALL_ARGS+=(--port "$PORT")
fi

deneb "${INSTALL_ARGS[@]}"

# Step 5: Verify
echo ""
echo "Verifying gateway service..."
sleep 2
systemctl --user status deneb-gateway.service --no-pager || true

echo ""
echo "Gateway service installed. Useful commands:"
echo "  deneb gateway status     - Check gateway status"
echo "  deneb gateway restart    - Restart the gateway"
echo "  deneb gateway stop       - Stop the gateway"
echo "  systemctl --user status deneb-gateway  - systemd status"
echo "  journalctl --user -u deneb-gateway -f  - Follow logs"
