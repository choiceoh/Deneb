#!/usr/bin/env bash
# Start the Go gateway with the Node.js plugin host.
#
# Usage:
#   scripts/start-go-gateway.sh [--port PORT] [--bind MODE] [--daemon] [--force]
#
# The Go binary (dist/deneb-gateway) is the primary process.
# It spawns a Node.js plugin host subprocess via --plugin-host-cmd,
# which handles Telegram and other channel extensions over a Unix socket bridge.
#
# If the Go binary is not found, falls back to the Node.js gateway.
#
# Prerequisites:
#   - make go-binary (builds dist/deneb-gateway)
#   - pnpm build (builds dist/plugin-host/main.js)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Parse arguments.
PORT=""
BIND=""
DAEMON=""
FORCE=""
LOG_LEVEL=""
EXTRA_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --bind) BIND="$2"; shift 2 ;;
    --daemon) DAEMON="1"; shift ;;
    --force) FORCE="1"; shift ;;
    --log-level) LOG_LEVEL="$2"; shift 2 ;;
    *) EXTRA_ARGS+=("$1"); shift ;;
  esac
done

GO_BINARY="$REPO_DIR/dist/deneb-gateway"
PLUGIN_HOST_ENTRY="$REPO_DIR/dist/plugin-host/main.js"

# Check if the Go gateway binary exists.
if [[ ! -x "$GO_BINARY" ]]; then
  echo "Go gateway binary not found at $GO_BINARY"
  echo "Building with: make go-binary"
  cd "$REPO_DIR" && make go-binary
fi

# Check if the plugin host entry exists.
if [[ ! -f "$PLUGIN_HOST_ENTRY" ]]; then
  echo "Plugin host entry not found at $PLUGIN_HOST_ENTRY"
  echo "Build it with: pnpm build"
  exit 1
fi

# Build the Go gateway command.
CMD=("$GO_BINARY")
CMD+=("--plugin-host-cmd" "node $PLUGIN_HOST_ENTRY")

if [[ -n "$PORT" ]]; then
  CMD+=("--port" "$PORT")
fi
if [[ -n "$BIND" ]]; then
  CMD+=("--bind" "$BIND")
fi
if [[ -n "$DAEMON" ]]; then
  CMD+=("--daemon")
fi
if [[ -n "$LOG_LEVEL" ]]; then
  CMD+=("--log-level" "$LOG_LEVEL")
fi
CMD+=("${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}")

echo "Starting Go gateway: ${CMD[*]}"
exec "${CMD[@]}"
