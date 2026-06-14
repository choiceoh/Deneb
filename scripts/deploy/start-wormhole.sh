#!/usr/bin/env bash
# Start/stop the wormhole model router for Deneb.
#
# wormhole (cmd/wormhole) is the OpenAI/Anthropic-compatible router that fans one
# endpoint out to many backends (local vLLM + cloud) by model name. When Deneb's
# own roles point at it (deneb.json models.providers.wormhole), it sits in the
# model hot path, so it is run as a restart-on-failure service — prefer the
# systemd unit (scripts/deploy/wormhole.service) in production; this script is the
# manual / non-systemd path.
#
# Usage:
#   scripts/deploy/start-wormhole.sh start    # build + start in background
#   scripts/deploy/start-wormhole.sh stop     # stop
#   scripts/deploy/start-wormhole.sh status   # health check
#   scripts/deploy/start-wormhole.sh restart  # stop + start
#
# Config: ~/.wormhole/config.json (override with WORMHOLE_CONFIG). The listen
# address (default :18800) and token live there; see cmd/wormhole/config.example.json.

set -euo pipefail

CONFIG="${WORMHOLE_CONFIG:-$HOME/.wormhole/config.json}"
HEALTH_HOST="${WORMHOLE_HEALTH_HOST:-127.0.0.1}"
HEALTH_PORT="${WORMHOLE_HEALTH_PORT:-18800}"
LOG_FILE="/tmp/wormhole.log"
PID_FILE="/tmp/wormhole.pid"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BIN="$REPO_DIR/dist/wormhole"

build() {
    echo "building wormhole (make wormhole)..."
    (cd "$REPO_DIR" && make wormhole)
}

start() {
    if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "already running (pid $(cat "$PID_FILE"))"
        return 0
    fi
    if [[ ! -f "$CONFIG" ]]; then
        echo "ERROR: wormhole config not found at $CONFIG" >&2
        echo "       create it from gateway-go/cmd/wormhole/config.example.json" >&2
        return 1
    fi
    [[ -x "$BIN" ]] || build

    echo "starting wormhole (config $CONFIG)..."
    nohup "$BIN" --config "$CONFIG" > "$LOG_FILE" 2>&1 &
    local pid=$!
    echo "$pid" > "$PID_FILE"
    echo "started (pid $pid, log: $LOG_FILE)"

    local tries=0
    while (( tries < 30 )); do
        if curl -sf "http://$HEALTH_HOST:$HEALTH_PORT/health" > /dev/null 2>&1; then
            echo "healthy"
            return 0
        fi
        sleep 0.5
        (( tries++ ))
    done
    echo "WARNING: started but /health not passing after 15s — check $LOG_FILE" >&2
}

stop() {
    if [[ -f "$PID_FILE" ]]; then
        local pid
        pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "stopping (pid $pid)..."
            kill "$pid"
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
    if curl -sf "http://$HEALTH_HOST:$HEALTH_PORT/health" 2>/dev/null; then
        echo "  (wormhole healthy on $HEALTH_HOST:$HEALTH_PORT)"
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
