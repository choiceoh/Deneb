#!/usr/bin/env bash
# Start the Go gateway.
#
# Usage:
#   scripts/deploy/start-go-gateway.sh [--port PORT] [--bind MODE] [--daemon] [--force]
#
# Prerequisites:
#   - make go (builds the gateway binary)

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

GO_BINARY="$REPO_DIR/gateway-go/gateway"

# Check if the Go gateway binary exists.
if [[ ! -x "$GO_BINARY" ]]; then
  echo "Go gateway binary not found at $GO_BINARY"
  echo "Building with: make go"
  cd "$REPO_DIR" && make go
fi

# Build the Go gateway command.
CMD=("$GO_BINARY")

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

# Restart loop: allow one graceful restart (exit 75) per 30 minutes.
RESTART_INTERVAL=600
last_restart=0

echo "Starting Go gateway: ${CMD[*]}"
while true; do
  "${CMD[@]}"
  EXIT=$?
  if [ "$EXIT" -ne 75 ]; then
    exit "$EXIT"
  fi
  now=$(date +%s)
  elapsed=$(( now - last_restart ))
  if [ "$elapsed" -lt "$RESTART_INTERVAL" ]; then
    echo "Gateway requested restart but rate limit reached (last restart ${elapsed}s ago, limit ${RESTART_INTERVAL}s). Exiting."
    exit 75
  fi
  echo "Gateway requested restart (exit 75), restarting..."
  last_restart="$now"
  sleep 0.5
done
