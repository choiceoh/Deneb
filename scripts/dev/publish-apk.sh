#!/usr/bin/env bash
# Build a foss APK (debug or release variant) and publish it to the shared serve dir + version.json.
#
# Single entry point so concurrent agent worktrees don't clobber the publish dir:
#   - the APK filename carries the commit hash (see androidApp/build.gradle.kts),
#     so two builds at different commits never overwrite each other;
#   - version.json is regenerated here from the actual built artifact, so the
#     in-app updater always points at a real file with matching code/name.
#
# Concurrent publishes still race on version.json itself, but every APK is
# preserved by its hash, so any specific build stays retrievable by URL.
#
# Before building, it runs the native live-app smoke (native-app-smoke.sh) as a
# gate: that smoke walks the real screens in the Compose Desktop build of the
# same commonMain code the APK ships, catching render-time crashes that
# compileKotlinDesktop + unit tests miss (e.g. 158/#1959). A real crash aborts
# the publish; a harness that cannot start is a warning, not a block.
#
# Env:
#   DENEB_APK_DIR       publish dir            (default: ~/.cache/deneb-apk)
#   DENEB_APK_BASE_URL  base URL the app uses  (default: http://127.0.0.1:19010)
#   ANDROID_HOME        Android SDK            (default: ~/android-sdk)
#   DENEB_SKIP_SMOKE    set to skip the pre-publish smoke gate entirely
#   DENEB_APK_VARIANT   fossDebug (default) | fossRelease (R8-optimized, much faster)
#
# Usage:
#   scripts/dev/publish-apk.sh "release notes shown in the in-app updater"
set -euo pipefail

NOTES="${1:-}"
APK_DIR="${DENEB_APK_DIR:-$HOME/.cache/deneb-apk}"
BASE_URL="${DENEB_APK_BASE_URL:-http://127.0.0.1:19010}"
SDK="${ANDROID_HOME:-$HOME/android-sdk}"
# Build variant. fossDebug is the default (debuggable, no R8 — easy install but slow
# Compose). fossRelease is R8-optimized + non-debuggable (much smoother scroll); its
# signing falls back to the debug keystore when no release key is configured, so it
# installs in place over a debug build (same signature). The serve dir is scanned across
# both variants below so version codes stay monotonic when the two coexist.
#
# NOTE: AGP 9.2.1's assembleFossRelease emits a manifest-less, unsigned APK that will not
# install ("Missing AndroidManifest.xml"). The bundle's universal APK is the valid, signed,
# R8-minified release artifact, so fossRelease builds packageFossReleaseUniversalApk instead
# and copies that fixed-name output below.
VARIANT="${DENEB_APK_VARIANT:-fossDebug}"
case "$VARIANT" in
  fossDebug)   GRADLE_TASK="assembleFossDebug" ;;
  fossRelease) GRADLE_TASK="packageFossReleaseUniversalApk" ;;
  *) echo "unknown DENEB_APK_VARIANT: $VARIANT (use fossDebug or fossRelease)" >&2; exit 1 ;;
esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APP_DIR="$REPO_ROOT/client-android/app"
cd "$APP_DIR"

VERSION_NAME="$(sed -n 's/^appVersion = "\(.*\)"/\1/p' gradle/libs.versions.toml)"
LIBS_VERSION_CODE="$(sed -n 's/^android-versionCode = "\(.*\)"/\1/p' gradle/libs.versions.toml)"
SHA="$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)"

if [ -z "$VERSION_NAME" ] || [ -z "$LIBS_VERSION_CODE" ]; then
  echo "could not read appVersion/android-versionCode from libs.versions.toml" >&2
  exit 1
fi

# Auto-assign a collision-free versionCode instead of trusting a hand-bumped libs
# value. The 155/162/164 clobbers happened because each agent worktree bumped
# libs to whatever number it guessed, and two could pick the same. Here flock
# serializes the read-max -> build -> publish window against the shared serve
# dir, and each publish takes (serve-dir max + 1), never below the libs floor.
# -PdenebVersionCode feeds the chosen code into both gradle modules (androidApp
# versionCode + composeApp BuildKonfig), so the APK, its filename, and the in-app
# updater's DENEB_VERSION_CODE all agree. No manual libs bump needed.
mkdir -p "$APK_DIR"
exec 200>"$APK_DIR/.publish.lock"
flock 200
SERVE_MAX="$(ls "$APK_DIR"/deneb-*.apk 2>/dev/null \
  | grep -oE 'deneb-[0-9.]+-[0-9]+-' | grep -oE '[0-9]+-$' | tr -d '-' \
  | sort -n | tail -1)"
VERSION_CODE=$(( ${SERVE_MAX:-0} + 1 ))
if [ "$VERSION_CODE" -lt "$LIBS_VERSION_CODE" ]; then
  VERSION_CODE="$LIBS_VERSION_CODE"
fi
echo "auto-assigned versionCode $VERSION_CODE (serve max=${SERVE_MAX:-none}, libs floor=$LIBS_VERSION_CODE)"

# Pre-publish smoke gate. Render-time crashes (the #1959 class) only surface when
# the real screens compose with real data — which neither compileKotlinDesktop nor
# unit tests do. Run the live smoke before sinking time into the Android build and,
# more importantly, before shipping a crash to the phone.
SMOKE="$REPO_ROOT/scripts/dev/native-app-smoke.sh"
NATIVE_APP="$REPO_ROOT/scripts/dev/native-app.sh"
if [ -n "${DENEB_SKIP_SMOKE:-}" ]; then
  echo "pre-publish smoke: SKIPPED (DENEB_SKIP_SMOKE set) — render crashes will not be caught"
elif ! "$NATIVE_APP" start >/dev/null 2>&1; then
  # Harness unavailable (no Xvfb, contended display, …) is an infra gap, not a
  # code defect — warn loudly but don't block a legitimate publish on it.
  echo "WARNING: native live-app harness could not start — skipping pre-publish smoke" >&2
  echo "         (render crashes will NOT be caught; set DENEB_SKIP_SMOKE=1 to silence)" >&2
else
  echo "pre-publish smoke: walking real screens (native-app-smoke.sh) ..."
  if ! "$SMOKE"; then
    echo "" >&2
    echo "PRE-PUBLISH SMOKE FAILED — a screen crashed or rendered wrong. NOT publishing." >&2
    echo "  inspect the saved screenshots: ~/.cache/deneb-native/<instance>/shots/smoke-*.png" >&2
    echo "  (the live app is left running so you can probe it: $NATIVE_APP shot <name>)" >&2
    echo "  override and publish anyway: DENEB_SKIP_SMOKE=1 scripts/dev/publish-apk.sh ..." >&2
    exit 1
  fi
  "$NATIVE_APP" stop >/dev/null 2>&1 || true   # free the harness JVM on success
  echo "pre-publish smoke: PASS"
fi

echo "building deneb $VERSION_NAME ($VERSION_CODE) @ $SHA [$VARIANT] ..."
DENEB_BUILD_SHA="$SHA" ANDROID_HOME="$SDK" ./gradlew ":androidApp:$GRADLE_TASK" -q -PdenebVersionCode="$VERSION_CODE"

APK_NAME="deneb-$VERSION_NAME-$VERSION_CODE-$SHA-$VARIANT.apk"
if [ "$VARIANT" = "fossRelease" ]; then
  # The bundle universal APK has a fixed name; copy + rename it to the serve name.
  APK_PATH="androidApp/build/outputs/apk_from_bundle/fossRelease/androidApp-foss-release-universal.apk"
else
  APK_PATH="androidApp/build/outputs/apk/foss/debug/$APK_NAME"
fi
if [ ! -f "$APK_PATH" ]; then
  echo "build did not produce $APK_PATH" >&2
  exit 1
fi

mkdir -p "$APK_DIR"
cp "$APK_PATH" "$APK_DIR/$APK_NAME"

# Escape backslashes and double quotes so arbitrary notes stay valid JSON.
NOTES_ESC="${NOTES//\\/\\\\}"
NOTES_ESC="${NOTES_ESC//\"/\\\"}"

cat > "$APK_DIR/version.json" <<EOF
{
  "code": $VERSION_CODE,
  "name": "$VERSION_NAME",
  "url": "$BASE_URL/$APK_NAME",
  "notes": "$NOTES_ESC"
}
EOF

echo "published $APK_NAME"
echo "  apk  -> $APK_DIR/$APK_NAME"
echo "  json -> $APK_DIR/version.json (url=$BASE_URL/$APK_NAME)"
