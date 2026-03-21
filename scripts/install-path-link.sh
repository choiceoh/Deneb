#!/usr/bin/env bash
# Create a symlink so the `deneb` command is available system-wide.
#
# Usage:
#   scripts/install-path-link.sh
#
# Attempts (in order):
#   1. npm link (creates a global npm symlink)
#   2. Symlink into ~/.local/bin (user-local, no sudo)
#   3. Symlink into /usr/local/bin (system-wide, requires sudo)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENTRY="$REPO_DIR/deneb.mjs"

if [[ ! -f "$ENTRY" ]]; then
  echo "Error: $ENTRY not found. Run from the repo root." >&2
  exit 1
fi

chmod +x "$ENTRY"

# Check if already available
if command -v deneb &>/dev/null; then
  EXISTING="$(command -v deneb)"
  echo "deneb is already in PATH: $EXISTING"
  exit 0
fi

# Try npm link first
if command -v npm &>/dev/null; then
  echo "Attempting npm link..."
  if (cd "$REPO_DIR" && npm link 2>/dev/null); then
    echo "Success: deneb linked via npm."
    echo "Verify: $(command -v deneb 2>/dev/null || echo 'restart your shell and run: deneb --version')"
    exit 0
  fi
fi

# Fallback: ~/.local/bin
LOCAL_BIN="$HOME/.local/bin"
mkdir -p "$LOCAL_BIN"
ln -sf "$ENTRY" "$LOCAL_BIN/deneb"
echo "Symlinked: $LOCAL_BIN/deneb -> $ENTRY"

if echo "$PATH" | tr ':' '\n' | grep -qx "$LOCAL_BIN"; then
  echo "deneb is now available."
else
  # Append to shell profile
  PROFILE=""
  if [[ -f "$HOME/.bashrc" ]]; then
    PROFILE="$HOME/.bashrc"
  elif [[ -f "$HOME/.profile" ]]; then
    PROFILE="$HOME/.profile"
  elif [[ -f "$HOME/.zshrc" ]]; then
    PROFILE="$HOME/.zshrc"
  fi

  EXPORT_LINE='export PATH="$HOME/.local/bin:$PATH"'
  if [[ -n "$PROFILE" ]]; then
    if ! grep -qF '.local/bin' "$PROFILE" 2>/dev/null; then
      echo "$EXPORT_LINE" >> "$PROFILE"
      echo "Added PATH entry to $PROFILE"
    fi
  fi

  echo "Run this in your current shell (or restart it):"
  echo "  $EXPORT_LINE"
fi
