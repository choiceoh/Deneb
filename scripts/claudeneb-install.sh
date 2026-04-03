#!/usr/bin/env bash
# Install Claudeneb as a desktop app (icon + launcher).
set -euo pipefail

DESKTOP_FILE="/usr/share/applications/claudeneb.desktop"
LAUNCHER="/usr/local/bin/claudeneb"

echo "Installing Claudeneb desktop app..."

# Launcher script — wraps claude-desktop with Deneb env vars.
# DENEB_HOST: set to DGX Spark IP for remote use, defaults to localhost.
sudo tee "$LAUNCHER" > /dev/null << 'SCRIPT'
#!/usr/bin/env bash
DENEB_HOST="${DENEB_HOST:-127.0.0.1}"
export ANTHROPIC_BASE_URL="http://${DENEB_HOST}:18789"
export CLAUDENEB_OPENAI_URL="${CLAUDENEB_OPENAI_URL:-http://${DENEB_HOST}:30000/v1}"
exec claude-desktop "$@"
SCRIPT
sudo chmod +x "$LAUNCHER"

# Desktop entry — shows up in app launcher with its own icon.
sudo tee "$DESKTOP_FILE" > /dev/null << 'DESKTOP'
[Desktop Entry]
Name=Claudeneb
Comment=Claude Desktop + Deneb
Exec=/usr/local/bin/claudeneb %u
Icon=claude-desktop
Type=Application
Terminal=false
Categories=Office;Utility;Development;
StartupWMClass=Claude
DESKTOP

# Refresh desktop database.
sudo update-desktop-database /usr/share/applications 2>/dev/null || true

echo "Done. 'Claudeneb' is now in your app launcher."
