#!/usr/bin/env bash
# setup-lmtp-socket.sh — cut the LMTP mail-ingest socket over to systemd socket
# activation so it survives the gateway's SIGUSR1 hot-restarts.
#
# WHY: every auto-deploy hot-restarts the gateway (~10s). Mail forwarded by the
# on-box Maddy server that lands in that window gets "connection refused", which
# Maddy's queue misclassifies as permanent and drops (lost a real mail 2026-06-16).
# With systemd owning the socket, those SYNs queue in the kernel backlog and are
# accepted when the gateway comes back.
#
# ORDER MATTERS:
#   1. Deploy a gateway build that contains the socket-activation code
#      (internal/platform/lmtpd/systemd_socket.go). It is a no-op until this
#      script runs, so deploying it first is safe.
#   2. Run this script ONCE on the gateway host. It causes a brief (~seconds)
#      one-time gateway downtime to hand the socket to systemd.
#
# Idempotent: safe to re-run. Rollback instructions print at the end.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_DIR="$HOME/.config/systemd/user"
DROPIN_DIR="$UNIT_DIR/deneb-gateway.service.d"
SERVICE="deneb-gateway.service"
SOCKET="deneb-lmtp.socket"

mkdir -p "$UNIT_DIR" "$DROPIN_DIR"

echo "==> installing $SOCKET"
install -m 0644 "$SCRIPT_DIR/deneb-lmtp.socket" "$UNIT_DIR/$SOCKET"

echo "==> installing gateway drop-in (Sockets= + ordering)"
cat > "$DROPIN_DIR/lmtp-socket.conf" <<'EOF'
# Receive deneb-lmtp.socket's fd via sd_listen_fds, and order the gateway after
# the socket so the fd is available at every (re)start. Installed by
# scripts/systemd/setup-lmtp-socket.sh.
[Unit]
Requires=deneb-lmtp.socket
After=deneb-lmtp.socket

[Service]
Sockets=deneb-lmtp.socket
EOF

systemctl --user daemon-reload

# The running gateway already holds 127.0.0.1:10024, so the socket unit cannot
# bind it while the gateway is up. Cut over with a brief stop: gateway down →
# socket binds → gateway back up inheriting the fd. After this one-time dip,
# every future restart reuses the systemd-held socket with no downtime.
echo "==> enabling $SOCKET"
systemctl --user enable "$SOCKET"

echo "==> cutover: stop gateway, start socket, start gateway"
systemctl --user stop "$SERVICE"
systemctl --user start "$SOCKET"
systemctl --user start "$SERVICE"

sleep 2
echo ""
echo "==> verification"
systemctl --user status "$SOCKET" --no-pager | sed -n '1,4p' || true
echo "--- gateway log (LMTP listen line) ---"
journalctl --user -u "$SERVICE" --since "30 seconds ago" --no-pager 2>/dev/null \
  | grep -iE "LMTP 서버 수신|systemd 소켓" | tail -3 \
  || echo "(no LMTP listen log yet — check 'journalctl --user -u $SERVICE -f')"

cat <<EOF

Done. The gateway now receives its LMTP socket from systemd; it survives restarts.
A line containing "systemd 소켓 활성화" in the gateway log confirms socket activation.

Rollback (revert to the gateway binding the port itself):
  systemctl --user disable --now $SOCKET
  rm -f "$DROPIN_DIR/lmtp-socket.conf" "$UNIT_DIR/$SOCKET"
  systemctl --user daemon-reload
  systemctl --user restart $SERVICE
EOF
