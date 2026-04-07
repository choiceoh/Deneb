#!/usr/bin/env bash
# deploy.sh — Pull latest main and restart production gateway.
# Usage: scripts/deploy/deploy.sh [--build-only]
set -euo pipefail

PROD_DIR="$HOME/deneb"
PROD_PORT=18789
LOG_FILE="/tmp/deneb-gateway.log"

cd "$PROD_DIR"

# Ensure we're on main
branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
    echo "ERROR: production must be on main (currently on $branch)" >&2
    exit 1
fi

# Pull latest
echo "==> git pull"
git pull --ff-only origin main

# Build
echo "==> make gateway-dgx"
make gateway-dgx

if [[ "${1:-}" == "--build-only" ]]; then
    echo "==> build done (--build-only, skipping restart)"
    exit 0
fi

# Restart
echo "==> restarting gateway (port $PROD_PORT)"
pkill -9 -f deneb-gateway || true
sleep 1
nohup ./gateway-go/deneb-gateway --bind loopback --port "$PROD_PORT" > "$LOG_FILE" 2>&1 &

# Verify
sleep 2
if curl -sf "http://127.0.0.1:$PROD_PORT/health" > /dev/null; then
    echo "==> deploy OK (pid $(pgrep -f deneb-gateway), port $PROD_PORT)"
else
    echo "ERROR: gateway not responding on :$PROD_PORT" >&2
    tail -20 "$LOG_FILE"
    exit 1
fi
