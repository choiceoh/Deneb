#!/usr/bin/env bash
# deploy.sh — Pull latest main and restart production gateway.
# Usage: scripts/deploy/deploy.sh [--build-only]
set -euo pipefail

PROD_DIR="$HOME/deneb"
PROD_PORT=18789
LOG_FILE="/tmp/deneb-gateway.log"
LOG_ARCHIVE_DIR="/tmp/deneb-gateway-logs"
LOG_ARCHIVE_KEEP=20   # keep last N pre-restart logs; older ones get pruned
LOG_ARCHIVE_MAX_BYTES=$((200 * 1024 * 1024))  # cap archive dir at 200MB

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
# Prefer port-based detection so we catch both the built binary AND any
# `go run` instance that was started manually (whose cmdline path lives
# under /tmp/go-build... and does not contain "deneb-gateway").
existing_pid=$(ss -ltnpH "sport = :$PROD_PORT" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | head -1 || true)
if [[ -z "$existing_pid" ]]; then
    existing_pid=$(pgrep -f 'dist/deneb-gateway' || true)
fi
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
# Rotate the previous log before starting the new gateway. Truncating
# (`>`) on every restart lost the entire pre-restart log, so postmortems
# of "what happened just before the restart" had nothing to work with —
# that bit us debugging the 4/24 19:00 cron miss. Now: move the current
# log to an archive dir with a timestamp, then append to a fresh LOG_FILE.
# Archive dir is pruned so disk never grows unbounded.
if [[ -s "$LOG_FILE" ]]; then
    mkdir -p "$LOG_ARCHIVE_DIR"
    stamp=$(date +%Y%m%d-%H%M%S)
    mv "$LOG_FILE" "$LOG_ARCHIVE_DIR/deneb-gateway-$stamp.log"
    # Best-effort gzip of the just-archived file (async so deploy is not
    # blocked on I/O). Keep the most recent one uncompressed for quick tail.
    (
        gzip -f "$LOG_ARCHIVE_DIR/deneb-gateway-$stamp.log" 2>/dev/null || true
    ) &
fi

# Prune archive: keep the newest LOG_ARCHIVE_KEEP files AND respect the
# total-size cap. `ls -t` sorts by mtime newest-first; tail -n +N drops the
# first N-1 (the ones we keep) and we delete the rest.
if [[ -d "$LOG_ARCHIVE_DIR" ]]; then
    # By count
    # shellcheck disable=SC2012
    ls -t "$LOG_ARCHIVE_DIR"/deneb-gateway-*.log* 2>/dev/null \
        | tail -n +$((LOG_ARCHIVE_KEEP + 1)) \
        | xargs -r rm -f
    # By total size — delete oldest until under cap.
    while :; do
        total=$(du -sb "$LOG_ARCHIVE_DIR" 2>/dev/null | awk '{print $1+0}')
        [[ -z "$total" || "$total" -le "$LOG_ARCHIVE_MAX_BYTES" ]] && break
        # shellcheck disable=SC2012 # filenames are deneb-gateway-*.log(.gz), alphanumeric only
        oldest=$(ls -tr "$LOG_ARCHIVE_DIR"/deneb-gateway-*.log* 2>/dev/null | head -n 1)
        [[ -z "$oldest" ]] && break
        rm -f "$oldest"
    done
fi

nohup ./dist/deneb-gateway --bind loopback --port "$PROD_PORT" >> "$LOG_FILE" 2>&1 &

# Verify
sleep 2
if curl -sf "http://127.0.0.1:$PROD_PORT/health" > /dev/null; then
    echo "==> deploy OK (pid $(pgrep -f deneb-gateway), port $PROD_PORT)"
else
    echo "ERROR: gateway not responding on :$PROD_PORT" >&2
    tail -20 "$LOG_FILE"
    exit 1
fi
