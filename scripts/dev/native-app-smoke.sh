#!/usr/bin/env bash
# native-app-smoke.sh — boot the live native app and walk the key screens,
# flagging runtime crashes/errors that compileKotlinDesktop + unit tests cannot
# catch. This is the class of bug that shipped in 158 (#1959): a LazyColumn
# duplicate-key IllegalArgumentException that only blew up at render time with
# real data.
#
# Why not golden screenshots: the harness is prod-connected (live mail/calendar
# data), so pixel diffs are non-deterministic. Instead, for each screen this:
#   1. drives to it (read-only: tap + Escape, never send/type/mutate),
#   2. screenshots it as an artifact for human review (Read the PNGs), and
#   3. asserts no NEW exception/crash line hit the app log while it rendered,
#      and that the app process is still alive.
#
# Run this before publishing an APK (scripts/dev/publish-apk.sh). It needs the
# live harness (Xvfb + prod gateway), so it is a manual pre-release gate, not CI.
#
# Usage:   scripts/dev/native-app-smoke.sh
# Env:     same as native-app.sh — set DENEB_GATEWAY_URL to target a dev gateway
#          instead of prod for deterministic runs.
#
# Nav coordinates below are phone-profile (412x915) pixels, mapped from the
# drawer + top bar. If the navigation layout changes, re-map them (boot the app,
# `native-app.sh shot`, Read the PNG, read off the new coords).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NA="$HERE/native-app.sh"
LOG="${HOME}/.cache/deneb-native/app.log"
SHOTS="${HOME}/.cache/deneb-native/shots"
# app_jvm.pid is the live JVM pid (from the window) — a stable aliveness probe.
# (native-app.sh status re-searches the window each call, which flakes right
# after a tap; kill -0 on the recorded pid does not.)
PIDFILE="${HOME}/.cache/deneb-native/app_jvm.pid"

# Crash-class signals. Kept specific so handled/info logging does not false-flag;
# "already used" is the exact #1959 Compose duplicate-key signature.
ERR_RE='Exception in thread|FATAL|Caused by:|already used|IllegalArgumentException|IllegalStateException|NullPointerException|ConcurrentModification|UninitializedProperty'

fail=0
results=()

log_lines() { wc -l < "$LOG" 2>/dev/null || echo 0; }

# check_screen NAME LINES_BEFORE — shot the screen, then scan the log lines that
# appeared since LINES_BEFORE for crash signals, and confirm the app is alive.
check_screen() {
  local name="$1" before="$2"
  "$NA" shot "$name" >/dev/null 2>&1 || true
  local newerr
  newerr="$(tail -n "+$((before + 1))" "$LOG" 2>/dev/null | grep -nE "$ERR_RE" | head -5 || true)"
  if [ -n "$newerr" ]; then
    results+=("FAIL  $name")
    echo "  ✗ $name — new crash-class log lines:"
    echo "$newerr" | sed 's/^/        /'
    fail=1
  elif ! kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null; then
    results+=("DEAD  $name")
    echo "  ✗ $name — app JVM (pid $(cat "$PIDFILE" 2>/dev/null)) is gone (hard crash?)"
    fail=1
  else
    results+=("ok    $name")
    echo "  ✓ $name"
  fi
}

echo "==> booting live app (idempotent) ..."
"$NA" start >/dev/null 2>&1 || { echo "native-app.sh start failed"; exit 1; }
sleep 3

# Known state: chat tab, dismiss any overlay/drawer.
"$NA" tap 165 27 >/dev/null 2>&1 || true
"$NA" key Escape >/dev/null 2>&1 || true
sleep 2

echo "==> walking key screens (read-only) ..."

# Main/chat screen — the work feed renders here, which is exactly where #1959 hit.
b="$(log_lines)"; check_screen "smoke-01-chat-workfeed" "$b"

# Left drawer destinations (verified coords): mail/calendar/search/people/categories.
nav=("smoke-02-mail:80:75" "smoke-03-calendar:80:133" "smoke-04-search:80:193" "smoke-05-people:80:253" "smoke-06-categories:80:313")
for entry in "${nav[@]}"; do
  IFS=: read -r nm x y <<<"$entry"
  "$NA" tap 25 37 >/dev/null 2>&1 || true   # open hamburger drawer
  sleep 1
  b="$(log_lines)"
  "$NA" tap "$x" "$y" >/dev/null 2>&1 || true  # open screen
  sleep 3                                       # let prod data load + list render
  check_screen "$nm" "$b"
  "$NA" key Escape >/dev/null 2>&1 || true      # back to chat
  sleep 1
done

# Settings screen + tabs (gateway default, then 모델/크론/토픽문서/알림).
"$NA" tap 235 27 >/dev/null 2>&1 || true
sleep 2
b="$(log_lines)"; check_screen "smoke-07-settings" "$b"
tabs=("smoke-08-models:150:149" "smoke-09-crons:215:149" "smoke-10-topicdocs:295:149" "smoke-11-alerts:370:149")
for entry in "${tabs[@]}"; do
  IFS=: read -r nm x y <<<"$entry"
  b="$(log_lines)"
  "$NA" tap "$x" "$y" >/dev/null 2>&1 || true
  sleep 2
  check_screen "$nm" "$b"
done
"$NA" tap 165 27 >/dev/null 2>&1 || true   # back to chat
sleep 1

# Right-side recent-sessions drawer (the screen retired/leaked topics in the past).
b="$(log_lines)"
"$NA" tap 388 37 >/dev/null 2>&1 || true
sleep 2
check_screen "smoke-12-sessions" "$b"
"$NA" key Escape >/dev/null 2>&1 || true

echo
echo "==> smoke summary"
printf '   %s\n' "${results[@]}"
echo "   shots: $SHOTS/smoke-*.png  (Read them to eyeball each screen)"
if [ "$fail" -eq 0 ]; then
  echo "PASS — every key screen rendered with no crash-class log lines"
else
  echo "FAIL — a screen crashed/errored (see above); Read its smoke-*.png"
fi
exit "$fail"
