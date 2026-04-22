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
echo "==> make gateway-prod"
make gateway-prod

if [[ "${1:-}" == "--build-only" ]]; then
    echo "==> build done (--build-only, skipping restart)"
    exit 0
fi

# Restart — graceful first (SIGTERM, up to 10s), then SIGKILL as fallback.
# This gives active agent runs a chance to finish instead of being killed
# mid-turn, which otherwise leaves half-delivered replies in Telegram.
# Matches the Hermes-style restart convention: long-running streams drain
# in-flight work before exit.
echo "==> restarting gateway (port $PROD_PORT)"
existing_pid=$(pgrep -f 'dist/deneb-gateway' || true)
if [[ -n "$existing_pid" ]]; then
    echo "    graceful SIGTERM → pid $existing_pid (up to 10s drain)"
    kill -TERM "$existing_pid" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        if ! kill -0 "$existing_pid" 2>/dev/null; then
            break
        fi
        sleep 1
    done
    if kill -0 "$existing_pid" 2>/dev/null; then
        echo "    still alive after 10s → SIGKILL"
        kill -KILL "$existing_pid" 2>/dev/null || true
        sleep 1
    fi
fi
nohup ./dist/deneb-gateway --bind loopback --port "$PROD_PORT" > "$LOG_FILE" 2>&1 &

# Verify
sleep 2
if curl -sf "http://127.0.0.1:$PROD_PORT/health" > /dev/null; then
    echo "==> deploy OK (pid $(pgrep -f deneb-gateway), port $PROD_PORT)"
else
    echo "ERROR: gateway not responding on :$PROD_PORT" >&2
    tail -20 "$LOG_FILE"
    exit 1
fi
