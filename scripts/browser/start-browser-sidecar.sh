#!/usr/bin/env bash
# Deneb browser sidecar launcher — Xvfb display + resident headful Chromium
# (Playwright persistent profile) + loopback HTTP API on :18930.
#
# Human login/takeover: run `view` to expose the SAME browser over noVNC,
# log into sites there, close the tab — the agent keeps the sessions.
#
#   start-browser-sidecar.sh start|stop|status|view|logs
set -euo pipefail

DISP="${DENEB_BROWSER_DISPLAY:-:98}"
PORT="${DENEB_BROWSER_PORT:-18930}"
VNC_PORT="${DENEB_BROWSER_VNC_PORT:-5998}"
NOVNC_PORT="${DENEB_BROWSER_NOVNC_PORT:-6098}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_DIR="${HOME}/.cache/deneb-browser"
mkdir -p "$STATE_DIR"

log() { echo "==> $*"; }
have() { command -v "$1" >/dev/null 2>&1; }

start_xvfb() {
  if ! pgrep -f "Xvfb $DISP " >/dev/null; then
    log "starting Xvfb on $DISP"
    Xvfb "$DISP" -screen 0 1280x900x24 >"$STATE_DIR/xvfb.log" 2>&1 &
    sleep 1
  fi
}

start() {
  have node || { echo "node 미설치"; exit 1; }
  [ -d "$DIR/node_modules/playwright" ] || { log "installing deps"; (cd "$DIR" && npm install --no-fund --no-audit); }
  start_xvfb
  if pgrep -f "node $DIR/server.mjs" >/dev/null; then
    log "already running"
  else
    log "starting sidecar on 127.0.0.1:$PORT (display $DISP)"
    DISPLAY="$DISP" DENEB_BROWSER_PORT="$PORT" nohup node "$DIR/server.mjs" \
      >"$STATE_DIR/sidecar.log" 2>&1 &
  fi
  for _ in $(seq 1 30); do
    curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1 && { log "healthy"; return; }
    sleep 1
  done
  echo "sidecar did not become healthy — see $STATE_DIR/sidecar.log" >&2
  exit 1
}

stop() {
  pkill -f "node $DIR/server.mjs" 2>/dev/null || true
  pkill -f "x11vnc -display $DISP " 2>/dev/null || true
  pkill -f "websockify.*:$NOVNC_PORT" 2>/dev/null || true
  log "stopped (Xvfb는 유지 — 완전 정리는 pkill -f 'Xvfb $DISP')"
}

status() {
  pgrep -f "node $DIR/server.mjs" >/dev/null && echo "sidecar: running" || echo "sidecar: stopped"
  curl -sf "http://127.0.0.1:$PORT/health" 2>/dev/null || true
  echo
}

view() {
  for b in x11vnc websockify; do
    have "$b" || { echo "missing '$b' — sudo apt-get install -y x11vnc novnc websockify"; exit 1; }
  done
  start_xvfb
  if ! pgrep -f "x11vnc -display $DISP " >/dev/null; then
    x11vnc -display "$DISP" -rfbport "$VNC_PORT" -localhost -nopw -forever -shared -quiet \
      -bg -o "$STATE_DIR/x11vnc.log" >/dev/null 2>&1 || true
  fi
  if ! pgrep -f "websockify.*:$NOVNC_PORT" >/dev/null; then
    websockify --web /usr/share/novnc "$NOVNC_PORT" "localhost:$VNC_PORT" \
      >"$STATE_DIR/novnc.log" 2>&1 &
  fi
  ip="$(tailscale ip -4 2>/dev/null | head -1 || hostname -I | awk '{print $1}')"
  log "noVNC: http://$ip:$NOVNC_PORT/vnc.html — 여기서 로그인하면 에이전트가 그 세션으로 열람합니다"
}

logs() { tail -n 50 "$STATE_DIR/sidecar.log"; }

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  status) status ;;
  view) view ;;
  logs) logs ;;
  *) echo "usage: $0 start|stop|status|view|logs"; exit 1 ;;
esac
