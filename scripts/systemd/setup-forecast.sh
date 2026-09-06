#!/usr/bin/env bash
# Install the Chronos-2 forecast sidecar on the production host (srv4): the
# resident service on :8005 plus the gateway drop-in that makes the `forecast`
# tool exist at all (no DENEB_FORECAST_URL = the tool is never registered).
#
# Idempotent. Rollback = remove the drop-in and restart the gateway; the tool
# disappears and nothing else changes.
#
# Prereqs on the host (one-time, operator-space):
#   python3 -m venv ~/venvs/chronos
#   ~/venvs/chronos/bin/pip install "chronos-forecasting>=2.3" torch
#   HF_HUB_DISABLE_XET=1 ~/venvs/chronos/bin/hf download amazon/chronos-2
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-forecast.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"
GATEWAY_DROPIN_DIR="$USER_SYSTEMD_DIR/deneb-gateway.service.d"
PORT=8005

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-forecast must be run from the production main checkout." >&2
  exit 1
fi

if [[ ! -x "$HOME/venvs/chronos/bin/python" ]]; then
  echo "ERROR: ~/venvs/chronos missing — see the prereqs at the top of this script." >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR" "$GATEWAY_DROPIN_DIR"
install -m 0644 "$SCRIPT_DIR/deneb-forecast.service" "$USER_SYSTEMD_DIR/deneb-forecast.service"

cat > "$GATEWAY_DROPIN_DIR/forecast.conf" <<'CONF'
# Chronos-2 forecast sidecar (scripts/deploy/forecast-server.py, :8005).
# Setting this URL is what registers the `forecast` tool — without it the tool
# does not exist, which is the intended state on any host without the sidecar.
# Rollback: remove this drop-in, systemctl --user daemon-reload, restart the
# gateway. The sidecar itself can stay up; nothing else reads it.
[Service]
Environment=DENEB_FORECAST_URL=http://127.0.0.1:8005
CONF

systemctl --user daemon-reload
systemctl --user enable deneb-forecast.service
systemctl --user restart deneb-forecast.service

echo -n "waiting for :$PORT "
for _ in $(seq 1 60); do
  if curl -sf -m 2 "http://127.0.0.1:$PORT/health" >/dev/null; then
    echo "ready"
    curl -s "http://127.0.0.1:$PORT/health"
    echo
    break
  fi
  echo -n "."
  sleep 2
done

if ! curl -sf -m 2 "http://127.0.0.1:$PORT/health" >/dev/null; then
  echo >&2
  echo "ERROR: sidecar did not become healthy — journalctl --user -u deneb-forecast -n 50" >&2
  exit 1
fi

echo
echo "Chronos-2 forecast sidecar installed (port $PORT)."
echo "Cutover : the gateway picks up DENEB_FORECAST_URL on its next restart —"
echo "          kill -TERM \$(systemctl --user show -p MainPID --value deneb-gateway)"
echo "Rollback: rm $GATEWAY_DROPIN_DIR/forecast.conf && systemctl --user daemon-reload, then restart the gateway."
echo "Health  : curl -s http://127.0.0.1:$PORT/health"
