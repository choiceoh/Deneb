#!/usr/bin/env bash
# Width sweep for an andromeda surface — capture the SAME screen at several
# window widths in one run, so layout that only breaks at some sizes is visible
# instead of being found by accident.
#
# Why this exists: a single screenshot at the design width does not catch the
# most common layout defect in this repo — a rule that is fine at the width you
# happened to look at and wrong at another. In one session (2026-08-29) the
# Cygnus surface hit three of them: the thread rail misbehaving at 460px, the
# saved-window restore landing at 480px, and prose running the full width at
# 1920px once the window became maximizable. Every one was found by hand.
#
#   scripts/dev/andromeda-widths.sh                       # Cygnus, default widths
#   scripts/dev/andromeda-widths.sh "/" /tmp/ws 1280 1920 # workstation, chosen widths
#
# Args: [url-path] [out-dir] [width ...]
# Env:  HEIGHT (default 820), ANDROMEDA_SWEEP_PORT (default 1429)
#
# This script starts and stops its OWN vite dev server on a private port rather
# than reusing one you already have running. That is deliberate: a leftover dev
# server from an earlier run serves the code it was started with, and shooting
# it silently "verifies" a build that no longer exists (hit for real, same
# session). Owning the server is what makes the output trustworthy. --strictPort
# means a busy port fails loudly instead of quietly moving somewhere else.
set -euo pipefail

URL_PATH=${1:-/?window=cygnus}
OUT_DIR=${2:-/tmp/andromeda-widths}
shift 2 2>/dev/null || true
WIDTHS=("$@")
if [ ${#WIDTHS[@]} -eq 0 ]; then
  # The sizes that actually decide Cygnus layout: below the dock breakpoint,
  # at the window minimum, the shipped default, and maximized.
  WIDTHS=(460 600 1100 1920)
fi
HEIGHT=${HEIGHT:-820}
PORT=${ANDROMEDA_SWEEP_PORT:-1429}

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SHOT="$REPO_ROOT/scripts/dev/andromeda-shot.sh"
[ -x "$SHOT" ] || { echo "error: $SHOT not executable" >&2; exit 1; }

mkdir -p "$OUT_DIR"

VITE_PID=""

# PIDs currently listening on our port. `pnpm dev` execs a wrapper that spawns
# the real node server as a CHILD: killing $! alone leaves that child holding
# the port (observed while writing this script), and the next run would then
# refuse to start — or worse, shoot a server it did not start. Reclaim by what
# actually owns the socket, not by the PID we happen to remember.
port_listeners() {
  ss -ltnp 2>/dev/null | grep ":${PORT} " | grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u
}

cleanup() {
  local pids
  pids=$(port_listeners)
  [ -n "$pids" ] && kill $pids 2>/dev/null || true
  if [ -n "$VITE_PID" ] && kill -0 "$VITE_PID" 2>/dev/null; then
    kill "$VITE_PID" 2>/dev/null || true
    wait "$VITE_PID" 2>/dev/null || true
  fi
  # Verify rather than assume: a surviving listener is the exact condition that
  # poisons the next run, so say so instead of exiting quietly.
  for _ in 1 2 3 4 5; do
    [ -z "$(port_listeners)" ] && return 0
    sleep 1
  done
  if [ -n "$(port_listeners)" ]; then
    echo "warning: :${PORT} still held after cleanup by pid(s): $(port_listeners | tr '\n' ' ')" >&2
  fi
}
trap cleanup EXIT INT TERM

if curl -sf -o /dev/null --max-time 2 "http://localhost:${PORT}/"; then
  echo "error: something already listens on :${PORT} — refusing to shoot a server this script did not start." >&2
  echo "       Set ANDROMEDA_SWEEP_PORT to a free port, or stop that process." >&2
  exit 1
fi

LOG=$(mktemp)
echo "==> starting private vite on :${PORT} (from current source)"
( cd "$REPO_ROOT/andromeda" && exec pnpm dev --port "$PORT" --strictPort ) > "$LOG" 2>&1 &
VITE_PID=$!

for _ in $(seq 1 60); do
  curl -sf -o /dev/null --max-time 1 "http://localhost:${PORT}/" && break
  if ! kill -0 "$VITE_PID" 2>/dev/null; then
    echo "error: vite exited before serving — log:" >&2
    tail -20 "$LOG" >&2
    exit 1
  fi
  sleep 1
done
if ! curl -sf -o /dev/null --max-time 2 "http://localhost:${PORT}/"; then
  echo "error: vite did not come up on :${PORT} within 60s — log:" >&2
  tail -20 "$LOG" >&2
  exit 1
fi

SLUG=$(printf '%s' "$URL_PATH" | tr -c 'A-Za-z0-9' '-' | sed 's/--*/-/g; s/^-//; s/-$//')
[ -n "$SLUG" ] || SLUG=root

echo "==> ${URL_PATH} @ ${WIDTHS[*]} (height ${HEIGHT})"
for w in "${WIDTHS[@]}"; do
  out="$OUT_DIR/${SLUG}-${w}.png"
  ANDROMEDA_DEV_PORT="$PORT" "$SHOT" "$URL_PATH" "$out" "${w}x${HEIGHT}" > /dev/null
  echo "  ${w}x${HEIGHT}  $out"
done

echo "==> done — read the PNGs above side by side"
