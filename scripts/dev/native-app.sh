#!/usr/bin/env bash
#
# native-app.sh — Run the REAL native client (client-android Compose Desktop
# target) headlessly on the server so AI agents (and humans) can see and drive
# the actual app — not static previews.
#
# Why this exists
# ---------------
# The native client (`client-android/`) is a Kotlin Multiplatform app whose
# commonMain is shared across Android / iOS / desktop. The desktop JVM target
# (`com.inspiredandroid.kai.MainKt`, window titled "Deneb") is the SAME app the
# phone runs, minus the Android shell. Running it under a virtual X display
# (Xvfb) lets us exercise the live UI — connected to the real gateway, with real
# mail / calendar / sessions — and capture it as PNG or drive it with synthetic
# input. This is "computer use" scoped to the Deneb native app.
#
# The previous headless option, `:composeApp:renderPreviews`, only renders
# stateless composables with MOCK data. This harness runs the WHOLE app live.
#
# Quick start (agent)
# -------------------
#   scripts/dev/native-app.sh start          # boot Xvfb + launch app (phone, prod)
#   scripts/dev/native-app.sh shot home      # capture -> ~/.cache/deneb-native/shots/home.png
#   scripts/dev/native-app.sh tap 200 120    # click at physical px (200,120)
#   scripts/dev/native-app.sh type "안녕"     # type text (tap a field first)
#   scripts/dev/native-app.sh key Return      # press a key
#   scripts/dev/native-app.sh view            # also expose noVNC for a human to watch
#   scripts/dev/native-app.sh stop            # tear everything down
#
# Coordinates are pixels as they appear in the screenshot (top-left origin). The
# phone profile is 412x915 — a real Galaxy S25's dp size. Linux Compose renders at
# density 1, so screenshot px == dp == xdotool coords: click the pixel you see.
#
# Nothing here modifies the app source: the gateway URL + client token are
# seeded into the desktop app's own encrypted settings (~/.kai/settings.aes),
# byte-compatible with EncryptedFileSettings.kt.
#
set -euo pipefail

# ── Paths ───────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APP_DIR="$REPO_ROOT/client-android/app"

# ── Per-instance isolation ───────────────────────────────────────────────────
# Many agent worktrees share this host. Without isolation they'd all fight over
# one X display (:99), one state dir, and one VNC port — so one session's
# `restart`/`stop` would kill another session's live app, and its shots would
# capture the wrong screen. Each instance (default: the worktree dir name) gets
# its own display, state dir, and ports, derived from a stable hash offset. Pin
# any of NATIVE_DISPLAY / DENEB_NATIVE_STATE / NATIVE_*_PORT to override.
INSTANCE="${DENEB_INSTANCE:-$(basename "$REPO_ROOT")}"
# cksum → a stable 0..39 offset; keeps displays in :99..:138 and ports clear of
# each other. Collisions are possible but rare (few agents run the app at once).
OFFSET=$(( $(printf '%s' "$INSTANCE" | cksum | cut -d' ' -f1) % 40 ))

STATE_DIR="${DENEB_NATIVE_STATE:-$HOME/.cache/deneb-native/$INSTANCE}"
SHOTS_DIR="$STATE_DIR/shots"
mkdir -p "$STATE_DIR" "$SHOTS_DIR"

# ── Config (override via env) ───────────────────────────────────────────────
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"
DISP="${NATIVE_DISPLAY:-:$((99 + OFFSET))}"
PROFILE_FILE="$STATE_DIR/profile"
VNC_PORT="${NATIVE_VNC_PORT:-$((5910 + OFFSET))}"
NOVNC_PORT="${NATIVE_NOVNC_PORT:-$((6080 + OFFSET))}"
TAILNET_IP="${NATIVE_TAILNET_IP:-100.105.145.6}"
GW_URL="${DENEB_GATEWAY_URL:-http://100.105.145.6:18789}"
SKIKO_RENDER="${NATIVE_SKIKO:-SOFTWARE}"   # SOFTWARE is safe on headless Xvfb (no GL)

# Profiles: NAME -> dpW dpH scale.  On Linux, Compose/Skiko ignores
# sun.java2d.uiScale, so density is fixed at 1 → physical px == dp. We render at
# true phone dp (a Galaxy S25 is ~412x915 dp); screenshot pixels map 1:1 to both
# dp and xdotool input coords, so "click the pixel you see" just works.
profile_geometry() {
  case "$1" in
    phone)   echo "412 915 1" ;;
    desktop) echo "1280 800 1" ;;
    *)       echo "" ;;
  esac
}

log()  { printf '\033[36m[native]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[native] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# ── Seed the desktop app's encrypted settings (gateway URL + client token) ───
# Mirrors EncryptedFileSettings.kt: ~/.kai/settings.key = 32 raw AES bytes,
# ~/.kai/settings.aes = IV(12) || AES-256-GCM(ct||tag) of a flat {String:String}
# JSON map. We MERGE so any existing keys the app wrote are preserved.
seed_settings() {
  local url="${1:-$GW_URL}"
  local token="${2:-}"
  if [[ -z "$token" ]]; then
    [[ -f "$HOME/.deneb/client_token" ]] || die "no token given and ~/.deneb/client_token missing"
    token="$(tr -d '\n' < "$HOME/.deneb/client_token")"
  fi
  python3 - "$url" "$token" <<'PY'
import os, sys, json
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
url, token = sys.argv[1], sys.argv[2]
kai = os.path.expanduser("~/.kai"); os.makedirs(kai, exist_ok=True)
keyp, aesp = os.path.join(kai, "settings.key"), os.path.join(kai, "settings.aes")
# Reuse the app's key if present so the app can still read what it wrote.
if os.path.exists(keyp) and os.path.getsize(keyp) == 32:
    key = open(keyp, "rb").read()
else:
    key = os.urandom(32); open(keyp, "wb").write(key)
data = {}
if os.path.exists(aesp):
    try:
        blob = open(aesp, "rb").read()
        iv, ct = blob[:12], blob[12:]
        data = json.loads(AESGCM(key).decrypt(iv, ct, None).decode())
    except Exception:
        data = {}  # corrupt/foreign key -> start fresh map
data["deneb.gatewayUrl"] = url
data["deneb.clientToken"] = token
iv = os.urandom(12)
ct = AESGCM(key).encrypt(iv, json.dumps(data).encode(), None)
open(aesp, "wb").write(iv + ct)
print(f"seeded ~/.kai/settings.aes  url={url}  token={token[:8]}…  keys={len(data)}")
PY
}

# ── Display lifecycle ───────────────────────────────────────────────────────
# NOTE: trailing `|| true` is load-bearing. Under `set -o pipefail`, pgrep/xdotool
# exit 1 when nothing matches, and a bare `x="$(app_wid)"` assignment would then
# trip `set -e` and kill the script the moment no window/process exists yet.
xvfb_pid()  { pgrep -f "Xvfb $DISP " | head -1 || true; }
app_wid()   { DISPLAY="$DISP" xdotool search --name "Deneb" 2>/dev/null | head -1 || true; }

# Re-assert window size/position until it sticks (Compose clobbers early resizes).
force_geometry() {
  local wid="$1" w="$2" h="$3" geo
  for _ in $(seq 1 15); do
    DISPLAY="$DISP" xdotool windowmove "$wid" 0 0 windowsize "$wid" "$w" "$h" 2>/dev/null || true
    sleep 0.4
    geo="$(DISPLAY="$DISP" xdotool getwindowgeometry "$wid" 2>/dev/null | awk '/Geometry/{print $2}')"
    [[ "$geo" == "${w}x${h}" ]] && return 0
  done
  log "warning: window geometry settled at ${geo:-?} (wanted ${w}x${h})"
}

start_xvfb() {
  local pxw="$1" pxh="$2"
  if [[ -n "$(xvfb_pid)" ]]; then log "Xvfb already on $DISP"; return; fi
  log "starting Xvfb on $DISP at ${pxw}x${pxh}x24"
  Xvfb "$DISP" -screen 0 "${pxw}x${pxh}x24" -nolisten tcp -ac >"$STATE_DIR/xvfb.log" 2>&1 &
  for _ in $(seq 1 50); do
    DISPLAY="$DISP" xdpyinfo >/dev/null 2>&1 && { log "Xvfb ready"; return; }
    sleep 0.2
  done
  die "Xvfb failed to come up (see $STATE_DIR/xvfb.log)"
}

# WM is tracked by pidfile (not a global `pgrep -x`) so stopping one instance
# never reaps another instance's WM running on a different display.
wm_running() { local f="$STATE_DIR/wm.pid"; [[ -f "$f" ]] && kill -0 "$(cat "$f" 2>/dev/null)" 2>/dev/null; }

# A minimal window manager is what makes keyboard focus reliable: without one,
# X input focus and Compose's in-app field focus drift apart between calls and
# typed keys vanish. The WM keeps the window active so a tap focuses a field and
# the next `type` lands in it. matchbox is a single-window kiosk WM — no title
# bar, no toolbar, it maximizes the one app to fill the screen → a clean phone
# frame. fluxbox is the fallback (it works but paints a title bar + toolbar).
start_wm() {
  [[ "${NATIVE_WM:-1}" == "0" ]] && return 0
  wm_running && return 0
  if have matchbox-window-manager; then
    log "starting matchbox window manager"
    DISPLAY="$DISP" matchbox-window-manager -use_titlebar no -use_cursor yes >"$STATE_DIR/wm.log" 2>&1 &
    echo $! >"$STATE_DIR/wm.pid"
  elif have fluxbox; then
    local fb="$STATE_DIR/fluxbox"; mkdir -p "$fb"
    printf 'session.screen0.toolbar.visible: false\nsession.screen0.focusModel: ClickToFocus\n' >"$fb/init"
    : >"$fb/keys"; : >"$fb/menu"
    log "starting fluxbox window manager (fallback — has chrome)"
    DISPLAY="$DISP" fluxbox -rc "$fb/init" -log "$fb/fluxbox.log" >/dev/null 2>&1 &
    echo $! >"$STATE_DIR/wm.pid"
  else
    log "no WM (matchbox/fluxbox) installed — keyboard focus may be flaky"
    return 0
  fi
  sleep 1
}

cmd_start() {
  local profile="${1:-phone}"
  local geo; geo="$(profile_geometry "$profile")"
  [[ -n "$geo" ]] || die "unknown profile '$profile' (use: phone | desktop)"
  read -r dpw dph scale <<<"$geo"
  # Optional explicit override (e.g. NATIVE_W=480 NATIVE_H=1040 for a roomier frame).
  dpw="${NATIVE_W:-$dpw}"; dph="${NATIVE_H:-$dph}"
  local pxw=$((dpw * scale)) pxh=$((dph * scale))
  echo "$profile $dpw $dph $scale $pxw $pxh" >"$PROFILE_FILE"

  for b in Xvfb xdotool scrot; do have "$b" || die "missing '$b' — run: sudo apt-get install -y xvfb xdotool scrot"; done
  [[ -d "$APP_DIR" ]] || die "app dir not found: $APP_DIR"

  seed_settings "$GW_URL" "" >&2
  start_xvfb "$pxw" "$pxh"
  start_wm

  local existing; existing="$(app_wid)"
  if [[ -n "$existing" ]]; then
    log "app already running — re-asserting ${pxw}x${pxh} geometry"
    force_geometry "$existing" "$pxw" "$pxh"
    DISPLAY="$DISP" xdotool getwindowpid "$existing" 2>/dev/null >"$STATE_DIR/app_jvm.pid" || true
    cmd_status; return
  fi

  log "launching native app (profile=$profile ${dpw}x${dph}dp @${scale}x → ${pxw}x${pxh}px, gateway=$GW_URL)"
  # Gradle defaults the build JVM to headless; the forked app JVM inherits it and
  # AWT throws HeadlessException on window creation. Force a real display. SOFTWARE
  # rendering avoids needing GL on the headless Xvfb screen. Cap the app heap: the
  # default max (25% of 128 GB ≈ 32 GB) blows the strict-overcommit budget on this
  # host (vm.overcommit_memory=2). The daemon keeps its own -Xmx4g (command-line
  # arg beats JAVA_TOOL_OPTIONS), so only the forked app JVM is capped here.
  local jvmopts="-Djava.awt.headless=false -Dskiko.renderApi=$SKIKO_RENDER -Xmx${NATIVE_APP_XMX:-1024m}"
  # setsid fully detaches gradle into its own session, so the live app survives
  # even if THIS start invocation is interrupted (e.g. a caller's timeout). The
  # gradle `run` task kills the forked app JVM when its client dies — without
  # setsid, interrupting `start` cascades into the app dying right after Koin init.
  (
    cd "$APP_DIR"
    DISPLAY="$DISP" ANDROID_HOME="$ANDROID_HOME" JAVA_TOOL_OPTIONS="$jvmopts" \
      setsid ./gradlew :composeApp:run --console=plain </dev/null >"$STATE_DIR/app.log" 2>&1 &
    echo $! >"$STATE_DIR/app.pid"
  )
  log "waiting for the Deneb window (first run compiles; up to ~4 min)…"
  local wid=""
  for _ in $(seq 1 240); do
    wid="$(app_wid)"; [[ -n "$wid" ]] && break
    if ! kill -0 "$(cat "$STATE_DIR/app.pid" 2>/dev/null)" 2>/dev/null; then
      tail -n 30 "$STATE_DIR/app.log" >&2 || true
      die "gradle :composeApp:run exited before a window appeared (see $STATE_DIR/app.log)"
    fi
    sleep 1
  done
  [[ -n "$wid" ]] || { tail -n 30 "$STATE_DIR/app.log" >&2; die "timed out waiting for window (see $STATE_DIR/app.log)"; }

  # Borderless full-bleed: fill the screen so the screenshot is exactly the app.
  # Compose re-applies its WindowState (main.kt opens 1280x800) on first
  # composition, clobbering an early resize — so re-assert until the geometry
  # sticks at the screen size.
  force_geometry "$wid" "$pxw" "$pxh"
  DISPLAY="$DISP" xdotool windowfocus "$wid" 2>/dev/null || true
  # Record the app JVM pid straight from the window, so teardown is by-PID and
  # never needs `pkill -f` (which self-kills any shell whose argv holds the pattern).
  DISPLAY="$DISP" xdotool getwindowpid "$wid" 2>/dev/null >"$STATE_DIR/app_jvm.pid" || true
  sleep 1  # let it settle / connect to gateway
  log "app window ready (wid=$wid)"
  cmd_status
}

# ── Observation ─────────────────────────────────────────────────────────────
cmd_shot() {
  [[ -n "$(xvfb_pid)" ]] || die "nothing running — 'native-app.sh start' first"
  local name="${1:-shot-$(date +%H%M%S)}"
  local out="$SHOTS_DIR/${name}.png"
  # Window is resized to fill the screen, so a full-screen grab == the app.
  DISPLAY="$DISP" scrot --overwrite "$out" 2>/dev/null || DISPLAY="$DISP" scrot -o "$out"
  log "saved $out"
  echo "$out"
}

# ── Synthetic input (xdotool). Coords = physical px in the screenshot. ───────
_wid_or_die() { local w; w="$(app_wid)"; [[ -n "$w" ]] || die "no app window (start first)"; echo "$w"; }

# Give the app window X keyboard focus ONLY if it doesn't already have it.
# Re-focusing a window that's already focused resets Compose's internal focus
# off the field a prior tap selected, so keystrokes would vanish — guard against it.
ensure_focus() {
  local wid="$1"
  [[ "$(DISPLAY="$DISP" xdotool getwindowfocus 2>/dev/null)" == "$wid" ]] && return 0
  DISPLAY="$DISP" xdotool windowfocus "$wid" 2>/dev/null || true
}

cmd_tap() {
  local x="$1" y="$2" wid; wid="$(_wid_or_die)"
  DISPLAY="$DISP" xdotool mousemove --window "$wid" "$x" "$y" click 1
  log "tap ($x,$y)"
}
cmd_dbltap() {
  local x="$1" y="$2" wid; wid="$(_wid_or_die)"
  DISPLAY="$DISP" xdotool mousemove --window "$wid" "$x" "$y" click --repeat 2 1
  log "double-tap ($x,$y)"
}
cmd_type() {
  local wid; wid="$(_wid_or_die)"
  ensure_focus "$wid"
  DISPLAY="$DISP" xdotool type --clearmodifiers --delay 35 -- "$*"
  log "type: $*"
}
cmd_key() {
  local wid; wid="$(_wid_or_die)"
  ensure_focus "$wid"
  DISPLAY="$DISP" xdotool key --clearmodifiers -- "$@"
  log "key: $*"
}
cmd_swipe() {  # X1 Y1 X2 Y2 [steps] — drag, e.g. to fling-scroll a list
  local x1="$1" y1="$2" x2="$3" y2="$4" wid; wid="$(_wid_or_die)"
  DISPLAY="$DISP" xdotool mousemove --window "$wid" "$x1" "$y1" mousedown 1 \
    mousemove --window "$wid" "$x2" "$y2" mouseup 1
  log "swipe ($x1,$y1)->($x2,$y2)"
}
cmd_scroll() {  # up|down [clicks]
  local dir="${1:-down}" n="${2:-3}" wid; wid="$(_wid_or_die)"
  read -r _ _ _ _ pxw pxh <"$PROFILE_FILE"
  local btn=5; [[ "$dir" == up ]] && btn=4
  DISPLAY="$DISP" xdotool mousemove --window "$wid" $((pxw/2)) $((pxh/2)) click --repeat "$n" "$btn"
  log "scroll $dir x$n"
}

# ── Optional: let a human watch/drive over Tailscale via noVNC ───────────────
cmd_view() {
  [[ -n "$(xvfb_pid)" ]] || die "nothing running — 'start' first"
  for b in x11vnc websockify; do have "$b" || die "missing '$b' — sudo apt-get install -y x11vnc novnc websockify"; done
  if ! pgrep -f "x11vnc -display $DISP " >/dev/null; then
    log "starting x11vnc (loopback:$VNC_PORT)"
    x11vnc -display "$DISP" -rfbport "$VNC_PORT" -localhost -nopw -forever -shared -quiet \
      -bg -o "$STATE_DIR/x11vnc.log" >/dev/null 2>&1 || true
  fi
  if ! pgrep -f "websockify.*:$NOVNC_PORT" >/dev/null; then
    local webroot=/usr/share/novnc
    log "starting noVNC (websockify $TAILNET_IP:$NOVNC_PORT)"
    nohup websockify --web="$webroot" "$TAILNET_IP:$NOVNC_PORT" "localhost:$VNC_PORT" \
      >"$STATE_DIR/novnc.log" 2>&1 &
    echo $! >"$STATE_DIR/novnc.pid"
    sleep 1
  fi
  log "open in a browser on the tailnet:"
  echo "  http://$TAILNET_IP:$NOVNC_PORT/vnc.html?autoconnect=1&resize=remote"
}

# ── Status / logs / teardown ────────────────────────────────────────────────
cmd_status() {
  local wid; wid="$(app_wid || true)"
  echo "instance:  $INSTANCE  (offset $OFFSET)"
  echo "display:   $DISP   (Xvfb pid: $(xvfb_pid || echo '-'))"
  if [[ -f "$PROFILE_FILE" ]]; then read -r p dpw dph s pxw pxh <"$PROFILE_FILE"; echo "profile:   $p  ${dpw}x${dph}dp @${s}x  → ${pxw}x${pxh}px"; fi
  echo "gateway:   $GW_URL"
  echo "app:       $([[ -n "$wid" ]] && echo "running (wid=$wid)" || echo 'not running')"
  echo "novnc:     $(pgrep -f "websockify.*:$NOVNC_PORT" >/dev/null && echo "http://$TAILNET_IP:$NOVNC_PORT/vnc.html?autoconnect=1&resize=remote" || echo 'off (run: native-app.sh view)')"
  echo "shots:     $SHOTS_DIR"
  echo "app log:   $STATE_DIR/app.log"
}
cmd_logs() { tail -n "${1:-40}" "$STATE_DIR/app.log" 2>/dev/null || die "no app log yet"; }

# Teardown is strictly by recorded PID / exact process name (comm) — never
# `pkill -f <pattern>`, which would also kill any shell whose command line
# happens to contain the pattern (including the one running this script).
kill_pidfile() { local f="$1"; [[ -f "$f" ]] || return 0; local p; p="$(cat "$f" 2>/dev/null)"; [[ -n "$p" ]] && { pkill -P "$p" 2>/dev/null || true; kill "$p" 2>/dev/null || true; }; rm -f "$f"; }

cmd_stop() {
  log "stopping…"
  kill_pidfile "$STATE_DIR/app_jvm.pid"   # the live app JVM (from the window)
  kill_pidfile "$STATE_DIR/app.pid"       # the gradlew launcher + its children
  kill_pidfile "$STATE_DIR/novnc.pid"     # websockify
  # Instance-scoped: x11vnc by its -display flag, WM by pidfile — never a global
  # pgrep -x that would reap another instance's WM/vnc on a different display.
  for p in $(pgrep -f "x11vnc -display $DISP " 2>/dev/null || true); do kill "$p" 2>/dev/null || true; done
  kill_pidfile "$STATE_DIR/wm.pid"
  local xp; xp="$(xvfb_pid)"; [[ -n "$xp" ]] && kill "$xp" 2>/dev/null || true
  log "stopped"
}

cmd_restart() { cmd_stop; sleep 1; cmd_start "${1:-phone}"; }

usage() {
  cat >&2 <<EOF
native-app.sh — run the real native client headlessly for agent verification

  start [phone|desktop]   boot Xvfb + seed gateway + launch the live app (default: phone)
  shot [name]             screenshot the app window  → $SHOTS_DIR/<name>.png
  tap X Y                 click at physical px (X,Y) as seen in the screenshot
  dbltap X Y              double-click
  type "text"             type text into the focused field (tap it first)
  key KEY [KEY...]        press key(s), e.g. Return / Escape / ctrl+a / BackSpace
  swipe X1 Y1 X2 Y2       drag (fling-scroll a list)
  scroll up|down [n]      mouse-wheel scroll at window center
  view                    expose noVNC over Tailscale so a human can watch/drive
  seed [url] [token]      (re)write ~/.kai gateway settings (defaults: prod)
  status | logs [n]       inspect
  restart [profile] | stop

Coords are screenshot pixels (phone profile = 412x915, 1px = 1dp). Connected to
the REAL gateway ($GW_URL) — actions hit real data (sending chat runs the agent).
EOF
  exit 1
}

cmd="${1:-}"; shift || true
case "$cmd" in
  start)   cmd_start "$@" ;;
  shot)    cmd_shot "$@" ;;
  tap)     cmd_tap "$@" ;;
  dbltap)  cmd_dbltap "$@" ;;
  type)    cmd_type "$@" ;;
  key)     cmd_key "$@" ;;
  swipe)   cmd_swipe "$@" ;;
  scroll)  cmd_scroll "$@" ;;
  view)    cmd_view "$@" ;;
  seed)    seed_settings "$@" ;;
  status)  cmd_status ;;
  logs)    cmd_logs "$@" ;;
  restart) cmd_restart "$@" ;;
  stop)    cmd_stop ;;
  *)       usage ;;
esac
