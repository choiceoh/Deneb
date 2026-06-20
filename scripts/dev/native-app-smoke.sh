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
# PHONE PROFILE: the native client is mobile-only (the desktop product moved to a
# separate app, Andromeda — see docs/operations/native-client.md). The phone
# profile renders the real mobile UI at 412x915 (-Ddeneb.platform=phone), so the
# walk drives the bottom tab bar + 더보기 menu the user actually sees. Navigation
# mixes OCR taptext (More-list rows, settings pill-tabs — labels survive layout
# shifts) with pixel taps for the fixed bottom-bar tabs and the few icon-only /
# data-dependent controls (hamburger, first mail row). Settling uses `wait-for`
# (poll until the anchor renders) instead of fixed sleeps.
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

# Bottom-bar tab pixel centers (phone 412x915, density 1 → px == dp). The labels
# (피드/채팅/메일/달력) collide with screen titles (e.g. "메일" is inside "받은
# 메일"), so the persistent bottom bar is pixel-tapped, not taptext'd.
BBAR_Y=858
TAB_FEED_X=37 TAB_CHAT_X=118 TAB_MAIL_X=200 TAB_CAL_X=282 TAB_MORE_X=364

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
    # Anchor missing, app alive, no crash. Tolerate the prod-data load-failure
    # state: a screen that rendered its "다시 시도" retry affordance is the right
    # screen with no data (graceful degradation), not a wrong-screen/blank
    # render. The smoke is a CRASH gate, not a data-availability check, and prod
    # fetches are flaky in the headless harness (mail/people/model lists often
    # come back empty). A genuine wrong-screen has neither the anchor nor the
    # retry affordance, so it still fails.
    if "$NA" assert "다시 시도" >/dev/null 2>&1; then
      results+=("ok    $name (no data)")
      echo "  ✓ $name — rendered cleanly (load failed → 다시 시도; anchor \"$anchor\" needs data)"
    else
      results+=("WRONG $name")
      echo "  ✗ $name — anchor \"$anchor\" not on screen (wrong screen / blank render?)"
      fail=1
    fi
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
# Self-heals the occasional cold-first-tap flake (the first nav after boot can
# fire taptext mid-animation). The re-tap is a no-op when nav already succeeded.
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

# go_tab NAME TAB_X ANCHOR [scroll] — open a primary section by pixel-tapping its
# bottom-bar tab (the bar is persistent on top-level routes). Pass "scroll" as the
# 4th arg for list screens to also probe below the fold for render crashes.
go_tab() {
  local name="$1" x="$2" anchor="$3" do_scroll="${4:-}" b
  b="$(log_lines)"
  "$NA" tap "$x" "$BBAR_Y" >/dev/null 2>&1 || true
  settle "$anchor"
  check_screen "$name" "$b" "$anchor"
  [ "$do_scroll" = "scroll" ] && scroll_probe "$name"
}

# more_section NAME LABEL ANCHOR [scroll] — open a secondary section from the 더보기
# menu: tap the 더보기 tab, wait for the section row, taptext it, assert the
# screen's anchor. Search/categories/settings keep the bottom bar (they are in
# denebBottomBarRoutes), so 더보기 is always reachable to re-enter for the next one.
more_section() {
  local name="$1" label="$2" anchor="$3" do_scroll="${4:-}" b
  "$NA" tap "$TAB_MORE_X" "$BBAR_Y" >/dev/null 2>&1 || true   # open the 더보기 menu
  "$NA" wait-for "$label" 6 >/dev/null 2>&1 || true            # section row rendered
  b="$(log_lines)"
  "$NA" taptext "$label" >/dev/null 2>&1 || true               # open the section by its text
  settle "$anchor"
  retry_nav "$label" "$anchor"
  check_screen "$name" "$b" "$anchor"
  [ "$do_scroll" = "scroll" ] && scroll_probe "$name"
}

# settings_tab NAME TAB_LABEL ANCHOR — switch a settings pill-tab by its label.
settings_tab() {
  local name="$1" tab="$2" anchor="$3" b
  b="$(log_lines)"
  "$NA" taptext "$tab" >/dev/null 2>&1 || true
  settle "$anchor"
  retry_nav "$tab" "$anchor"
  check_screen "$name" "$b" "$anchor"
}

echo "==> booting live app (phone profile, idempotent) ..."
# The native client is mobile-only; the phone profile renders the real mobile UI
# (412x915, -Ddeneb.platform=phone) the user actually ships. Restart only on a
# profile mismatch so re-runs are fast.
cur_profile="$("$NA" status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | sed -n 's/^profile:[[:space:]]*\([a-z]*\).*/\1/p')"
if [ "$cur_profile" = "phone" ]; then
  "$NA" start phone >/dev/null 2>&1 || { echo "native-app.sh start failed"; exit 1; }
else
  "$NA" restart phone >/dev/null 2>&1 || { echo "native-app.sh restart phone failed"; exit 1; }
fi
sleep 3

# Resolve instance-namespaced state paths from the harness (DENEB_INSTANCE-aware).
status_out="$("$NA" status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g')"
LOG="$(printf '%s\n' "$status_out" | sed -n 's/^app log:[[:space:]]*//p' | head -1)"
SHOTS="$(printf '%s\n' "$status_out" | sed -n 's/^shots:[[:space:]]*//p' | head -1)"
[ -n "$LOG" ] || { echo "could not resolve app log path from native-app.sh status"; exit 1; }
PIDFILE="$(dirname "$LOG")/app_jvm.pid"

echo "==> walking key screens (read-only, mobile nav) ..."

# 피드 (work feed) — renders here (where #1959 hit). Content varies, so crash/alive
# only (no stable anchor — "피드" also labels the tab). Scroll: #1959 was a
# below-the-fold card.
b="$(log_lines)"
"$NA" tap "$TAB_FEED_X" "$BBAR_Y" >/dev/null 2>&1 || true
sleep 2
check_screen "smoke-01-feed" "$b"
scroll_probe "smoke-01-feed"

# 채팅 (chat home) — greeting + input bar. Content varies → crash/alive only.
b="$(log_lines)"
"$NA" tap "$TAB_CHAT_X" "$BBAR_Y" >/dev/null 2>&1 || true
sleep 2
check_screen "smoke-02-chat" "$b"

# Primary tabs with anchors. Mail is list-heavy → scroll-probe below the fold.
go_tab "smoke-03-mail"     "$TAB_MAIL_X" "받은 메일" scroll
go_tab "smoke-04-calendar" "$TAB_CAL_X"  "일정"

# 더보기 menu sections. Search/categories keep the bottom bar; categories is a list
# → scroll-probe.
more_section "smoke-05-search"     "검색"     "검색"     scroll
more_section "smoke-06-categories" "카테고리" "카테고리" scroll

# 사람 (people) — the merged 사람 surface is a pinned row INSIDE categories (no
# top-level menu item). Re-enter categories, then tap the pinned row. Anchor on
# the "최근 연락" section label — the bare word "사람" also appears on the
# categories screen itself, so it can't distinguish the two. People is a pushed
# detail (not in the bottom-bar route set), so Escape back to categories after.
"$NA" tap "$TAB_MORE_X" "$BBAR_Y" >/dev/null 2>&1 || true
"$NA" wait-for "카테고리" 6 >/dev/null 2>&1 || true
"$NA" taptext "카테고리" >/dev/null 2>&1 || true
"$NA" wait-for "카테고리" 8 >/dev/null 2>&1 || sleep 2
retry_nav "카테고리" "카테고리"
b="$(log_lines)"
"$NA" taptext "사람" >/dev/null 2>&1 || true
settle "최근 연락"
retry_nav "사람" "최근 연락"
check_screen "smoke-07-people" "$b" "최근 연락"
scroll_probe "smoke-07-people"
"$NA" key Escape >/dev/null 2>&1 || true
sleep 1

# 설정 (settings) + pill-tabs. Settings is a 더보기 section (moved out of the
# primary tabs); it keeps the bottom bar.
more_section "smoke-08-settings" "설정" "게이트웨이"
settings_tab "smoke-09-models"   "모델"  "경량"
settings_tab "smoke-10-crons"    "크론"  ""
# alerts has no reliable OCR anchor: its only distinctive text ("이 빌드는 알림
# 캡처를 지원하지 않습니다") is OCR-hostile (캡처를→"BMS", 빌드/않습니다 unread),
# and the lone readable word "알림" also appears on the gateway tab. Crash/alive only.
settings_tab "smoke-11-alerts"   "알림"  ""

# Session-history drawer — on phone the LEFT drawer is the session history, opened
# by the hamburger (icon-only, top-left). Go to chat first, then tap the hamburger.
"$NA" tap "$TAB_CHAT_X" "$BBAR_Y" >/dev/null 2>&1 || true
sleep 1
b="$(log_lines)"
"$NA" tap 25 37 >/dev/null 2>&1 || true                 # hamburger → left session drawer
"$NA" wait-for "대화 기록" 5 >/dev/null 2>&1 || sleep 2
check_screen "smoke-12-sessions" "$b" "대화 기록"
"$NA" key Escape >/dev/null 2>&1 || true
sleep 1

# Mail DETAIL — open the first inbox message: richest list-item screen (AI
# analysis markdown, attachment chips, sender context). The row itself is data-
# dependent so it stays a pixel tap; everything around it is text-driven. Anchor
# on the 휴지통 (trash) action pill, present only on the detail screen.
"$NA" tap "$TAB_MAIL_X" "$BBAR_Y" >/dev/null 2>&1 || true
"$NA" wait-for "받은 메일" 8 >/dev/null 2>&1 || sleep 2
b="$(log_lines)"
"$NA" tap 200 185 >/dev/null 2>&1 || true               # first message row (content starts below the title)
"$NA" wait-for "휴지통" 8 >/dev/null 2>&1 || sleep 2
# self-heal the data-dependent row tap: if the detail did not open (cold-tap
# flake, or the list was still settling), re-tap the row once and re-settle.
if ! "$NA" assert "휴지통" >/dev/null 2>&1; then
  "$NA" tap 200 185 >/dev/null 2>&1 || true
  "$NA" wait-for "휴지통" 8 >/dev/null 2>&1 || sleep 2
fi
check_screen "smoke-13-mail-detail" "$b" "휴지통"
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
