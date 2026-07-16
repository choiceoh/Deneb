#!/usr/bin/env bash
# setup-http-socket.sh — cut the gateway HTTP listener over to systemd socket
# activation so it survives SIGUSR1 hot-restarts (deployment continuity phase 2).
#
# WHY: every auto-deploy hot-restarts the gateway. With the turn-aware idle gate
# (wait-idle.sh) the drain is near-instant, but new connections still get
# "connection refused" for the boot window (~2s). With systemd owning the
# socket, those SYNs queue in the kernel backlog and are accepted when the
# gateway comes back — zero refused connections. Mirrors the LMTP cutover
# (setup-lmtp-socket.sh, which closed the same window for mail ingest).
#
# ORDER MATTERS:
#   1. Deploy a gateway build that contains the socket-activation code
#      (gateway-go/internal/infra/sdsocket). It is a no-op until this script
#      runs, so deploying it first is safe.
#   2. Run this script ONCE on the gateway host (with the gateway running). It
#      hot-restarts the gateway via SIGUSR1 to hand the socket to systemd — a
#      brief HTTP connection-refused window during that one restart, then never
#      again.
#
# Idempotent: safe to re-run. Rollback instructions print at the end.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_DIR="$HOME/.config/systemd/user"
DROPIN_DIR="$UNIT_DIR/deneb-gateway.service.d"
SERVICE="deneb-gateway.service"
SOCKET="deneb-http.socket"

mkdir -p "$UNIT_DIR" "$DROPIN_DIR"

echo "==> installing $SOCKET"
install -m 0644 "$SCRIPT_DIR/deneb-http.socket" "$UNIT_DIR/$SOCKET"

echo "==> installing gateway drop-in (Sockets= + ordering)"
cat > "$DROPIN_DIR/http-socket.conf" <<'EOF'
# Receive deneb-http.socket's fd via sd_listen_fds, ordered after the socket.
# Wants= (not Requires=) so a socket-bind failure degrades to the gateway
# binding the port itself (the activation code is a no-op without LISTEN_*),
# rather than failing the gateway. Installed by scripts/systemd/setup-http-socket.sh.
[Unit]
After=deneb-http.socket
Wants=deneb-http.socket

[Service]
Sockets=deneb-http.socket
EOF

systemctl --user daemon-reload

# Enable (do NOT start) the socket: the running gateway still holds the port,
# so starting the socket now would fail to bind. The cutover restart below lets
# systemd start the socket in the right order instead.
echo "==> enabling $SOCKET"
systemctl --user enable "$SOCKET"

# Cut over via the gateway's own hot-restart (SIGUSR1 -> exit 75 -> systemd
# Restart=always), NOT systemctl stop/start: the production unit is
# RefuseManualStop/Start so stop/start is refused. On exit the gateway releases
# the port; because the drop-in adds After=/Wants=deneb-http.socket, systemd
# starts the socket first (binding the now-free port) and passes its fd to the
# new gateway. Brief connection-refused window during this one cutover restart;
# afterwards the systemd-held socket survives every restart with no window.
echo "==> cutover via SIGUSR1 hot-restart"
before_pid="$(systemctl --user show "$SERVICE" -p MainPID --value 2>/dev/null || echo 0)"
if [ "$before_pid" = "0" ] || [ -z "$before_pid" ]; then
  echo "ERROR: $SERVICE is not running — start it first, then re-run this cutover." >&2
  exit 1
fi
systemctl --user kill --kill-who=main -s SIGUSR1 "$SERVICE"
for _ in $(seq 1 30); do
  pid="$(systemctl --user show "$SERVICE" -p MainPID --value 2>/dev/null || echo 0)"
  [ -n "$pid" ] && [ "$pid" != "0" ] && [ "$pid" != "$before_pid" ] && break
  sleep 1
done
sleep 3

echo ""
echo "==> verification"
systemctl --user status "$SOCKET" --no-pager | sed -n '1,4p' || true
echo "--- gateway log (socket-activation line) ---"
if journalctl --user -u "$SERVICE" --since "40 seconds ago" --no-pager 2>/dev/null \
     | grep -qiE "systemd socket-activated"; then
  echo "OK: socket activation confirmed (http listener: systemd socket-activated)."
else
  echo "WARN: did not see 'systemd socket-activated'. The gateway may have fallen"
  echo "      back to self-bind (socket failed to bind first). Check:"
  echo "        systemctl --user status $SOCKET"
  echo "        journalctl --user -u $SERVICE -f"
fi

echo ""
echo "Rollback (returns to gateway self-bind on the NEXT restart):"
echo "  systemctl --user disable --now $SOCKET"
echo "  rm $DROPIN_DIR/http-socket.conf && systemctl --user daemon-reload"
echo "  systemctl --user kill --kill-who=main -s SIGUSR1 $SERVICE"
