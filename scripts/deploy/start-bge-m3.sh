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
# bge-m3-server.py exposes --gpu-layers (llama.cpp parameter) rather than a
# CUDA/CPU --device toggle. Default to full offload (99 = all layers on GPU);
# override with BGE_M3_GPU_LAYERS=0 for CPU-only fallback.
GPU_LAYERS="${BGE_M3_GPU_LAYERS:-99}"
LOG_FILE="/tmp/bge-m3-server.log"
PID_FILE="/tmp/bge-m3-server.pid"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_SCRIPT="$SCRIPT_DIR/bge-m3-server.py"

# Pick a Python with uvicorn / fastapi / llama_cpp installed. The deploy
# venv at ~/venv carries them; the system python3 typically does not.
# Override with BGE_M3_PYTHON to point at a different interpreter.
PYTHON="${BGE_M3_PYTHON:-/home/choiceoh/venv/bin/python3}"
if [[ ! -x "$PYTHON" ]]; then
    echo "WARNING: $PYTHON not executable, falling back to system python3" >&2
    PYTHON=python3
fi

start() {
    if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "already running (pid $(cat "$PID_FILE"))"
        return 0
    fi

    echo "starting BGE-M3 server on $HOST:$PORT (gpu_layers=$GPU_LAYERS) via $PYTHON..."
    nohup "$PYTHON" "$SERVER_SCRIPT" \
        --port "$PORT" --host "$HOST" --gpu-layers "$GPU_LAYERS" \
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
