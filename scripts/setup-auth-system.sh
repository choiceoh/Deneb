#!/bin/bash
# Setup Deneb Auth Management System
# Run this once to set up:
# 1. Long-lived Claude Code token
# 2. Auth monitoring with notifications

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== Deneb Auth System Setup ==="
echo ""

# Step 1: Check current auth status
echo "Step 1: Checking current auth status..."
"$SCRIPT_DIR/claude-auth-status.sh" full || true
echo ""

# Step 2: Set up long-lived token
echo "Step 2: Long-lived token setup"
echo ""
echo "Option A: Use 'claude setup-token' (recommended)"
echo "  - Creates a long-lived API token"
echo "  - No daily re-auth needed"
echo "  - Run: claude setup-token"
echo ""
echo "Would you like to set up a long-lived token now? [y/N]"
read -r SETUP_TOKEN

if [[ "$SETUP_TOKEN" =~ ^[Yy] ]]; then
    echo ""
    echo "Opening https://console.anthropic.com/settings/api-keys"
    echo "Create a new key or copy existing one, then paste below."
    echo ""
    claude setup-token
fi

echo ""

# Step 3: Set up auth monitoring
echo "Step 3: Auth monitoring setup"
echo ""
echo "The auth monitor checks expiry every 30 minutes and notifies you."
echo ""
echo "Configure notification channels:"
echo ""

# Check for ntfy
echo "  ntfy.sh: Free push notifications to your phone"
echo "  1. Install ntfy app on your phone"
echo "  2. Subscribe to a topic (e.g., 'deneb-alerts')"
echo ""
echo "Enter ntfy.sh topic (or leave blank to skip):"
read -r NTFY_TOPIC

# Phone notification
echo ""
echo "  Deneb message: Send warning via Deneb itself"
echo "Enter your phone number for alerts (or leave blank to skip):"
read -r PHONE_NUMBER

# Update service file
SERVICE_FILE="$SCRIPT_DIR/systemd/deneb-auth-monitor.service"
if [ -n "$NTFY_TOPIC" ]; then
    sed -i "s|# Environment=NOTIFY_NTFY=.*|Environment=NOTIFY_NTFY=$NTFY_TOPIC|" "$SERVICE_FILE"
fi
if [ -n "$PHONE_NUMBER" ]; then
    sed -i "s|# Environment=NOTIFY_PHONE=.*|Environment=NOTIFY_PHONE=$PHONE_NUMBER|" "$SERVICE_FILE"
fi

# Install systemd units
echo ""
echo "Installing systemd timer..."
mkdir -p ~/.config/systemd/user
cp "$SCRIPT_DIR/systemd/deneb-auth-monitor.service" ~/.config/systemd/user/
cp "$SCRIPT_DIR/systemd/deneb-auth-monitor.timer" ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now deneb-auth-monitor.timer

echo "Auth monitor installed and running."
echo ""

# Summary
echo "=== Setup Complete ==="
echo ""
echo "What's configured:"
echo "  - Auth status: $SCRIPT_DIR/claude-auth-status.sh"
echo "  - Auth monitor: systemctl --user status deneb-auth-monitor.timer"
echo ""
echo "Quick commands:"
echo "  Check auth:  $SCRIPT_DIR/claude-auth-status.sh"
echo "  Test monitor: $SCRIPT_DIR/auth-monitor.sh"
echo ""
