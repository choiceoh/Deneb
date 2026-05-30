#!/usr/bin/env bash
#
# vendor-kai.sh — vendor the Kai UI (Apache-2.0) into client-android/app/ as the
# starting point for the Deneb native Android client (see README.md).
#
# Run on a machine with git + the Android/Kotlin toolchain. This only does the
# safe, mechanical part: clone Kai at a ref, copy it into app/ (minus .git), and
# record provenance. The Android-only strip and brain gut/rewire are manual,
# build-driven steps documented in README.md (they need Gradle feedback).
#
# Usage:
#   ./vendor-kai.sh                 # vendor Kai @ main into ./app
#   KAI_REF=<sha> ./vendor-kai.sh   # pin a specific commit (recommended)
#   ./vendor-kai.sh --force         # overwrite an existing ./app
#
set -euo pipefail

KAI_REPO="${KAI_REPO:-https://github.com/SimonSchubert/Kai.git}"
KAI_REF="${KAI_REF:-main}"

DEST="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$DEST/app"

force=false
[ "${1:-}" = "--force" ] && force=true

if [ -d "$APP_DIR" ] && [ -n "$(ls -A "$APP_DIR" 2>/dev/null)" ] && [ "$force" != true ]; then
  echo "error: $APP_DIR already exists and is not empty. Re-run with --force to overwrite." >&2
  exit 1
fi

# Disk-backed temp dir under $DEST: /tmp is often tmpfs (RAM) and too small to
# check out a full clone, which fails mid-checkout.
tmp="$(mktemp -d "$DEST/.vendor.XXXXXX")"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

echo "==> Cloning $KAI_REPO ($KAI_REF)..."
if [ "$KAI_REF" = "main" ] || [ "$KAI_REF" = "master" ]; then
  git clone --depth 1 --branch "$KAI_REF" "$KAI_REPO" "$tmp/kai"
else
  # Full clone then checkout the pinned ref (shallow checkout of an arbitrary
  # SHA is not always available from the server).
  git clone "$KAI_REPO" "$tmp/kai"
  git -C "$tmp/kai" checkout --quiet "$KAI_REF"
fi

sha="$(git -C "$tmp/kai" rev-parse HEAD)"
echo "==> Vendoring at $sha"

rm -rf "$tmp/kai/.git"
rm -rf "$APP_DIR"
mkdir -p "$APP_DIR"
cp -R "$tmp/kai/." "$APP_DIR/"

cat > "$DEST/VENDOR.txt" <<EOF
Vendored from: $KAI_REPO
Commit:        $sha
Vendored on:   $(date -u +%Y-%m-%dT%H:%M:%SZ)
License:       Apache-2.0 (see app/ for the upstream LICENSE)

Re-vendor with: KAI_REF=$sha ./vendor-kai.sh --force
EOF

echo "==> Done. Vendored tree in $APP_DIR"
echo "    Next: follow README.md (Step 2 strip Android-only, Step 3 swap the brain)."
