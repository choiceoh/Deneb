#!/usr/bin/env bash
set -euo pipefail

cd /repo

export DENEB_STATE_DIR="/tmp/deneb-test"
export DENEB_CONFIG_PATH="${DENEB_STATE_DIR}/deneb.json"

echo "==> Build"
pnpm build

echo "==> Seed state"
mkdir -p "${DENEB_STATE_DIR}/credentials"
mkdir -p "${DENEB_STATE_DIR}/agents/main/sessions"
echo '{}' >"${DENEB_CONFIG_PATH}"
echo 'creds' >"${DENEB_STATE_DIR}/credentials/marker.txt"
echo 'session' >"${DENEB_STATE_DIR}/agents/main/sessions/sessions.json"

echo "==> Reset (config+creds+sessions)"
pnpm deneb reset --scope config+creds+sessions --yes --non-interactive

test ! -f "${DENEB_CONFIG_PATH}"
test ! -d "${DENEB_STATE_DIR}/credentials"
test ! -d "${DENEB_STATE_DIR}/agents/main/sessions"

echo "==> Recreate minimal config"
mkdir -p "${DENEB_STATE_DIR}/credentials"
echo '{}' >"${DENEB_CONFIG_PATH}"

echo "==> Uninstall (state only)"
pnpm deneb uninstall --state --yes --non-interactive

test ! -d "${DENEB_STATE_DIR}"

echo "OK"
