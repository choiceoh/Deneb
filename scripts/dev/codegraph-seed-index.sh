#!/usr/bin/env bash
# Copy a donor .codegraph into dest, skip daemon runtime, then sync-run.
# Usage: codegraph-seed-index.sh <donor-.codegraph> <dest-repo-root>
set -uo pipefail

DONOR="${1:-}"
DEST="${2:-}"
[[ -n "$DONOR" && -d "$DONOR" && -n "$DEST" && -d "$DEST" ]] || exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$DEST/.codegraph"
for item in "$DONOR"/*; do
  [[ -e "$item" ]] || continue
  base=$(basename "$item")
  case "$base" in
    daemon.pid|daemon.sock|daemon.log) continue ;;
  esac
  cp -r "$item" "$DEST/.codegraph/" 2>/dev/null || true
done
bash "$HERE/codegraph-sync-run.sh" "$DEST"
exit 0
