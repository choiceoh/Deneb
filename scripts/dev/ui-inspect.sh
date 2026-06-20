#!/usr/bin/env bash
# ui-inspect.sh — vision-free UI inspection + drive. Dumps a screen's Compose semantics
# tree as TEXT (every node: text / role / clickable / bounds — the same tree accessibility
# uses, so Korean is exact, unlike OCR) and optionally drives it (click by node text),
# re-dumping after each action so a state change is visible. Lets an AI agent that is NOT
# a vision model verify and manipulate the mobile UI without reading PNGs.
#
# Headless + deterministic (mock data, Mobile.Android phone profile). Siblings:
#   renderPreviews   — the same screens as PNGs (needs a vision model to read)
#   native-app.sh    — the LIVE app on prod data, driven by pixel taps + OCR
#
# Usage:
#   scripts/dev/ui-inspect.sh                       # list available screens
#   scripts/dev/ui-inspect.sh <screen> [actions] [--dark]
#
# Examples:
#   scripts/dev/ui-inspect.sh todo                  # dump the to-do list
#   scripts/dev/ui-inspect.sh counter "click:증가;click:증가"   # drive + watch state change
#   scripts/dev/ui-inspect.sh settings --dark
#
# actions: a ';'-separated list of `click:<text>` (substring match) or `dump`.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$HERE/../../client-android/app"
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"

screen="${1:-}"
actions=""
dark="false"
for a in "${@:2}"; do
  case "$a" in
    --dark) dark="true" ;;
    *) actions="$a" ;;
  esac
done

cd "$APP_DIR"
# -q hushes Gradle's own logging; the harness's stdout passes through. Slice out just the
# inspector's block (between its markers) so the agent reads clean text.
./gradlew -q :composeApp:previewInspect \
  -Pscreen="$screen" -Pactions="$actions" -Pdark="$dark" \
  --console=plain 2>/dev/null |
  sed -n '/^=== UI-INSPECT /,/^=== UI-INSPECT END ===$/p'
