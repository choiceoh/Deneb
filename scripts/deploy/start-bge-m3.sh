#!/usr/bin/env bash
# Start/stop BGE-M3 embedding server for Deneb compaction fallback.
#
# Usage:
#   scripts/deploy/start-bge-m3.sh start   # start in background
#   scripts/deploy/start-bge-m3.sh stop    # stop
#   scripts/deploy/start-bge-m3.sh status  # health check
#   scripts/deploy/start-bge-m3.sh restart # stop + start

set -euo pipefail

PORT="${BGE_M3_PORT:-8001}"
HOST="${BGE_M3_HOST:-127.0.0.1}"
DEVICE="${BGE_M3_DEVICE:-cuda}"
LOG_FILE="/tmp/bge-m3-server.log"
PID_FILE="/tmp/bge-m3-server.pid"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_SCRIPT="$SCRIPT_DIR/bge-m3-server.py"

start() {
    if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "already running (pid $(cat "$PID_FILE"))"
        return 0
    fi

    echo "starting BGE-M3 server on $HOST:$PORT (device=$DEVICE)..."
    nohup python3 "$SERVER_SCRIPT" \
        --port "$PORT" --host "$HOST" --device "$DEVICE" \
        > "$LOG_FILE" 2>&1 &

    local pid=$!
    echo "$pid" > "$PID_FILE"
    echo "started (pid $pid, log: $LOG_FILE)"

    # Wait for health.
    local tries=0
    while (( tries < 120 )); do
        if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
            echo "healthy"
            return 0
        fi
        sleep 1
        (( tries++ ))
    done

    echo "WARNING: server started but health check not passing after 120s"
    echo "check $LOG_FILE for details"
}

stop() {
    if [[ -f "$PID_FILE" ]]; then
        local pid
        pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "stopping (pid $pid)..."
            kill "$pid"
            # Wait for exit.
            local tries=0
            while kill -0 "$pid" 2>/dev/null && (( tries < 10 )); do
                sleep 0.5
                (( tries++ ))
            done
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null || true
            fi
            echo "stopped"
        else
            echo "not running (stale pid file)"
        fi
        rm -f "$PID_FILE"
    else
        echo "not running"
    fi
}

status() {
    if curl -sf "http://$HOST:$PORT/health" 2>/dev/null; then
        echo ""  # curl already printed JSON
    else
        echo "unhealthy or not running"
        return 1
    fi
}

case "${1:-}" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; start ;;
    status)  status ;;
    *)
        echo "usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
