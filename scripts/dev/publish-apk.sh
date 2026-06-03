#!/usr/bin/env bash
# Build a foss-debug APK and publish it to the shared serve dir + version.json.
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
# Env:
#   DENEB_APK_DIR       publish dir            (default: ~/.cache/deneb-apk)
#   DENEB_APK_BASE_URL  base URL the app uses  (default: http://127.0.0.1:19010)
#   ANDROID_HOME        Android SDK            (default: ~/android-sdk)
#
# Usage:
#   scripts/dev/publish-apk.sh "release notes shown in the in-app updater"
set -euo pipefail

NOTES="${1:-}"
APK_DIR="${DENEB_APK_DIR:-$HOME/.cache/deneb-apk}"
BASE_URL="${DENEB_APK_BASE_URL:-http://127.0.0.1:19010}"
SDK="${ANDROID_HOME:-$HOME/android-sdk}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APP_DIR="$REPO_ROOT/client-android/app"
cd "$APP_DIR"

VERSION_NAME="$(sed -n 's/^appVersion = "\(.*\)"/\1/p' gradle/libs.versions.toml)"
VERSION_CODE="$(sed -n 's/^android-versionCode = "\(.*\)"/\1/p' gradle/libs.versions.toml)"
SHA="$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)"

if [ -z "$VERSION_NAME" ] || [ -z "$VERSION_CODE" ]; then
  echo "could not read appVersion/android-versionCode from libs.versions.toml" >&2
  exit 1
fi

echo "building deneb $VERSION_NAME ($VERSION_CODE) @ $SHA ..."
DENEB_BUILD_SHA="$SHA" ANDROID_HOME="$SDK" ./gradlew :androidApp:assembleFossDebug -q

APK_NAME="deneb-$VERSION_NAME-$VERSION_CODE-$SHA-fossDebug.apk"
APK_PATH="androidApp/build/outputs/apk/foss/debug/$APK_NAME"
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
