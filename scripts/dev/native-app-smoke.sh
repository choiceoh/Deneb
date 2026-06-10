#!/usr/bin/env bash
# native-app-smoke.sh — boot the live native app and walk the key screens,
# flagging runtime crashes/errors that compileKotlinDesktop + unit tests cannot
# catch. This is the class of bug that shipped in 158 (#1959): a LazyColumn
# duplicate-key IllegalArgumentException that only blew up at render time with
# real data.
#
# Per screen it asserts three things:
#   1. no NEW exception/crash line hit the app log while it rendered,
#   2. the app JVM is still alive, and
#   3. an OCR anchor text for that screen is actually on screen
#      (catches wrong-screen / blank render — not just crashes).
# Screenshots are saved as artifacts for human review (Read shots/smoke-*.png).
#
# Navigation is TEXT-DRIVEN via `native-app.sh taptext` (OCR) wherever a control
# has a label, so it survives layout shifts; only icon-only controls (hamburger,
# recent-sessions, a data-dependent list row) stay pixel-tapped. Settling uses
# `wait-for` (poll until the anchor renders) instead of fixed sleeps.
#
# READ-ONLY: taptext/tap + Escape only — never sends/types/mutates — so it is
# safe against the prod gateway. Run before publishing an APK. Needs the live
# harness (Xvfb + prod), so it is a manual pre-release gate, not CI.
#
# Usage: scripts/dev/native-app-smoke.sh
# Env:   same as native-app.sh — DENEB_GATEWAY_URL to target a dev gateway.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NA="$HERE/native-app.sh"
# Resolved from `native-app.sh status` after boot (DENEB_INSTANCE-aware).
LOG="" SHOTS="" PIDFILE=""

# Crash-class signals. Specific enough that handled/info logging doesn't false-
# flag; "already used" is the exact #1959 Compose duplicate-key signature.
ERR_RE='Exception in thread|FATAL|Caused by:|already used|IllegalArgumentException|IllegalStateException|NullPointerException|ConcurrentModification|UninitializedProperty'

fail=0
results=()
log_lines() { wc -l < "$LOG" 2>/dev/null || echo 0; }

# check_screen NAME LINES_BEFORE [ANCHOR]
check_screen() {
  local name="$1" before="$2" anchor="${3:-}"
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
  elif [ -n "$anchor" ] && ! "$NA" assert "$anchor" >/dev/null 2>&1; then
    # No crash, but the expected screen did not render — navigation went
    # somewhere wrong or the screen came up blank. A crash-only smoke misses
    # this (e.g. a tap that silently lands back on the chat screen).
    results+=("WRONG $name")
    echo "  ✗ $name — anchor \"$anchor\" not on screen (wrong screen / blank render?)"
    fail=1
  else
    results+=("ok    $name")
    echo "  ✓ $name${anchor:+  [anchor: $anchor]}"
  fi
}

# settle ANCHOR — wait for ANCHOR to render (≤8s); fall back to a short sleep so
# anchor-less screens still get a beat to paint.
settle() {
  [ -n "$1" ] && "$NA" wait-for "$1" 8 >/dev/null 2>&1 || sleep 2
}

# retry_nav LABEL ANCHOR — if ANCHOR didn't land, re-tap LABEL once and settle.
# Self-heals the occasional cold-first-tap flake (the very first drawer open after
# boot can fire taptext mid-animation). The re-tap is a no-op when nav already
# succeeded: LABEL (the English drawer/tab word) is only on screen while the
# drawer/tab-bar still shows it, so a successful nav leaves nothing to re-tap.
retry_nav() {
  local label="$1" anchor="$2"
  [ -n "$anchor" ] || return 0
  "$NA" assert "$anchor" >/dev/null 2>&1 && return 0
  "$NA" taptext "$label" >/dev/null 2>&1 || true
  settle "$anchor"
}

# scroll_probe NAME — scroll the current list down a few times, re-checking the
# log for new crash-class lines after each step. The top-of-list anchor check
# only renders the first viewport; a #1959-style LazyColumn duplicate-key crash
# can hide in a below-the-fold item that only composes once scrolled into view.
# Crash/alive only — scrolling moves content so an anchor would shift. Scrolling
# is read-only (a list "load more" is a GET), so it stays prod-safe.
scroll_probe() {
  local name="$1" b newerr step
  for step in 1 2 3; do
    b="$(log_lines)"
    "$NA" scroll down 6 >/dev/null 2>&1 || true
    sleep 1
    newerr="$(tail -n "+$((b + 1))" "$LOG" 2>/dev/null | grep -nE "$ERR_RE" | head -3 || true)"
    if [ -n "$newerr" ]; then
      results+=("FAIL  scroll-$name (step $step)")
      echo "  ✗ scroll-$name — crash while scrolling (step $step):"
      echo "$newerr" | sed 's/^/        /'
      fail=1; return
    fi
    if ! kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null; then
      results+=("DEAD  scroll-$name (step $step)")
      echo "  ✗ scroll-$name — app died while scrolling (step $step)"
      fail=1; return
    fi
  done
  "$NA" shot "$name-scrolled" >/dev/null 2>&1 || true
  results+=("ok    scroll-$name")
  echo "  ✓ scroll-$name — scrolled ${step}× clean (below-the-fold items rendered)"
}

# go_drawer NAME DRAWER_LABEL ANCHOR [scroll] — open a left-drawer screen by its
# label. Pass "scroll" as the 4th arg for list screens to also probe below the
# fold for render crashes.
go_drawer() {
  local name="$1" label="$2" anchor="$3" do_scroll="${4:-}" b
  "$NA" tap 25 37 >/dev/null 2>&1 || true            # hamburger (icon → pixel)
  "$NA" wait-for "$label" 5 >/dev/null 2>&1 || true  # drawer rendered
  b="$(log_lines)"
  "$NA" taptext "$label" >/dev/null 2>&1 || true     # open the screen by its text
  settle "$anchor"
  retry_nav "$label" "$anchor"
  check_screen "$name" "$b" "$anchor"
  [ "$do_scroll" = "scroll" ] && scroll_probe "$name"
  "$NA" key Escape >/dev/null 2>&1 || true            # back to chat
  sleep 1
}

# settings_tab NAME TAB_LABEL ANCHOR — switch a settings tab by its label.
settings_tab() {
  local name="$1" tab="$2" anchor="$3" b
  b="$(log_lines)"
  "$NA" taptext "$tab" >/dev/null 2>&1 || true
  settle "$anchor"
  retry_nav "$tab" "$anchor"
  check_screen "$name" "$b" "$anchor"
}

echo "==> booting live app (idempotent) ..."
"$NA" start >/dev/null 2>&1 || { echo "native-app.sh start failed"; exit 1; }
sleep 3

# Resolve instance-namespaced state paths from the harness (DENEB_INSTANCE-aware).
status_out="$("$NA" status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g')"
LOG="$(printf '%s\n' "$status_out" | sed -n 's/^app log:[[:space:]]*//p' | head -1)"
SHOTS="$(printf '%s\n' "$status_out" | sed -n 's/^shots:[[:space:]]*//p' | head -1)"
[ -n "$LOG" ] || { echo "could not resolve app log path from native-app.sh status"; exit 1; }
PIDFILE="$(dirname "$LOG")/app_jvm.pid"

# Known state: chat tab (taptext the toggle; pixel fallback if OCR misses).
"$NA" taptext "채팅" >/dev/null 2>&1 || "$NA" tap 165 27 >/dev/null 2>&1 || true
"$NA" key Escape >/dev/null 2>&1 || true
sleep 2

echo "==> walking key screens (read-only, text-driven nav) ..."

# Chat / work-feed — work feed renders here (where #1959 hit). Content varies, so
# crash/alive only (no stable anchor). Scroll it: #1959 was a below-the-fold card.
b="$(log_lines)"; check_screen "smoke-01-chat-workfeed" "$b"
scroll_probe "smoke-01-chat-workfeed"

# Left-drawer screens: tap the English drawer label, assert the screen's anchor.
# List-heavy screens (mail/categories) also scroll-probe below the fold.
go_drawer "smoke-02-mail"       "mail"       "받은 메일"  scroll
go_drawer "smoke-03-calendar"   "calendar"   "일정"
go_drawer "smoke-04-search"     "search"     "검색"
go_drawer "smoke-05-categories" "categories" "카테고리"  scroll

# People — the merged 사람 surface is a pinned row INSIDE categories now (no
# drawer item). Re-enter categories fresh (go_drawer escaped back to chat and
# scroll-probed the list away from the top), then tap the pinned row. Anchor on
# the screen's "최근 연락" section label — the bare word "사람" also appears on
# the categories screen itself, so it can't distinguish the two.
"$NA" tap 25 37 >/dev/null 2>&1 || true
"$NA" wait-for "categories" 5 >/dev/null 2>&1 || true
"$NA" taptext "categories" >/dev/null 2>&1 || true
"$NA" wait-for "카테고리" 8 >/dev/null 2>&1 || sleep 2
retry_nav "categories" "카테고리"
b="$(log_lines)"
"$NA" taptext "사람" >/dev/null 2>&1 || true
settle "최근 연락"
retry_nav "사람" "최근 연락"
check_screen "smoke-06-people" "$b" "최근 연락"
scroll_probe "smoke-06-people"
"$NA" key Escape >/dev/null 2>&1 || true
"$NA" key Escape >/dev/null 2>&1 || true
sleep 1

# Settings screen + tabs: taptext the toggle, then each tab label.
"$NA" taptext "설정" >/dev/null 2>&1 || "$NA" tap 235 27 >/dev/null 2>&1 || true
"$NA" wait-for "게이트웨이" 6 >/dev/null 2>&1 || sleep 2
b="$(log_lines)"; check_screen "smoke-07-settings" "$b" "게이트웨이"
settings_tab "smoke-08-models"    "모델"     "경량"
settings_tab "smoke-09-crons"     "크론"     ""
# alerts has no reliable OCR anchor: its only distinctive text ("이 빌드는 알림
# 캡처를 지원하지 않습니다", the desktop-unsupported notice) is OCR-hostile
# (캡처를→"BMS", 빌드/않습니다 unread), and the lone readable word "알림" also
# appears on the gateway tab. Crash/alive check only.
settings_tab "smoke-11-alerts"    "알림"     ""
"$NA" taptext "채팅" >/dev/null 2>&1 || "$NA" tap 165 27 >/dev/null 2>&1 || true
sleep 1

# Right-side recent-sessions drawer (icon → pixel).
b="$(log_lines)"
"$NA" tap 388 37 >/dev/null 2>&1 || true
"$NA" wait-for "대화 기록" 5 >/dev/null 2>&1 || sleep 2
check_screen "smoke-12-sessions" "$b" "대화 기록"
"$NA" key Escape >/dev/null 2>&1 || true
sleep 1

# Mail DETAIL — open the first inbox message: richest list-item screen (AI
# analysis markdown, attachment chips, sender context). The row itself is data-
# dependent so it stays a pixel tap; everything around it is text-driven.
"$NA" tap 25 37 >/dev/null 2>&1 || true
"$NA" wait-for "mail" 5 >/dev/null 2>&1 || true
"$NA" taptext "mail" >/dev/null 2>&1 || true
"$NA" wait-for "받은 메일" 8 >/dev/null 2>&1 || sleep 2
retry_nav "mail" "받은 메일"                  # ensure the inbox list is up before the row tap
b="$(log_lines)"
# First message row. The Deneb-idiom inbox header (← glyph + ultralight title +
# count line + section label) is taller than the old Material header, so the
# first row now spans roughly y 160-250; 210 aims at its center.
"$NA" tap 200 210 >/dev/null 2>&1 || true
"$NA" wait-for "보관" 8 >/dev/null 2>&1 || sleep 2
# self-heal the data-dependent row tap: if the detail did not open (cold-tap
# flake, or the list was still settling), re-tap the row once and re-settle.
if ! "$NA" assert "보관" >/dev/null 2>&1; then
  "$NA" tap 200 210 >/dev/null 2>&1 || true
  "$NA" wait-for "보관" 8 >/dev/null 2>&1 || sleep 2
fi
check_screen "smoke-13-mail-detail" "$b" "보관"
"$NA" key Escape >/dev/null 2>&1 || true
"$NA" key Escape >/dev/null 2>&1 || true

echo
echo "==> smoke summary"
printf '   %s\n' "${results[@]}"
echo "   shots: $SHOTS/smoke-*.png  (Read them to eyeball each screen)"
if [ "$fail" -eq 0 ]; then
  echo "PASS — every key screen rendered (anchor-verified) with no crash-class log lines"
else
  echo "FAIL — a screen crashed / went wrong (see above); Read its smoke-*.png"
fi
exit "$fail"
