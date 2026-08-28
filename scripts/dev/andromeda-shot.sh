#!/usr/bin/env bash
# Headless GUI verification for the andromeda web build (workstation + Cygnus)
# on the srv4 host — screenshot a running vite dev server with the playwright
# chromium already cached under ~/.cache/ms-playwright. No system installs.
#
# Usage:
#   scripts/dev/andromeda-shot.sh [url-path] [out.png] [WxH]
#   scripts/dev/andromeda-shot.sh "/?window=cygnus" /tmp/cygnus.png 480x700
#
# Prereq: `cd andromeda && pnpm dev` running on :1420. Use PLAIN dev, not
# dev:mock — the MSW service-worker gate delays React mount past the load
# event, so one-shot --screenshot captures a blank body (real finding,
# 2026-08-28). For populated screens, point plain dev at the dev gateway via
# VITE_GATEWAY_URL/VITE_GATEWAY_TOKEN instead of mock mode.
set -euo pipefail

URL_PATH=${1:-/?window=cygnus}
OUT=${2:-/tmp/andromeda-shot.png}
SIZE=${3:-480x700}
SIZE=${SIZE/x/,}

CHROME=$(ls -d "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux/chrome 2>/dev/null | sort -V | tail -1 || true)
if [ -z "$CHROME" ] || [ ! -x "$CHROME" ]; then
  echo "error: playwright chromium not found under ~/.cache/ms-playwright" >&2
  exit 1
fi
if ! curl -sf -o /dev/null --max-time 3 "http://localhost:1420/"; then
  echo "error: vite dev server not responding on :1420 — run 'cd andromeda && pnpm dev' first" >&2
  exit 1
fi

"$CHROME" --headless=new --no-sandbox --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --window-size="$SIZE" --virtual-time-budget=6000 \
  --screenshot="$OUT" "http://localhost:1420${URL_PATH}" 2>&1 | tail -1
echo "$OUT"
