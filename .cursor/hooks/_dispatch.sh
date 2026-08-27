#!/usr/bin/env bash
# Thin Cursor hook wrappers — keep hooks.json paths stable and short.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
export CURSOR_PROJECT_DIR="${CURSOR_PROJECT_DIR:-$ROOT}"
case "$(basename "$0")" in
  session-worktree.sh)
    exec bash "$ROOT/scripts/dev/cursor-worktree-init.sh"
    ;;
  guard-main-checkout.sh)
    exec bash "$ROOT/scripts/dev/cursor-worktree-guard.sh"
    ;;
  codegraph-sync.sh)
    exec bash "$ROOT/scripts/dev/cursor-codegraph-sync.sh"
    ;;
  codegraph-nudge.sh)
    exec bash "$ROOT/scripts/dev/cursor-hook-bridge.sh" \
      "$ROOT/scripts/dev/codegraph-nudge.py" nudge
    ;;
  codegraph-remind.sh)
    exec bash "$ROOT/scripts/dev/cursor-hook-bridge.sh" \
      "$ROOT/scripts/dev/codegraph-remind.py" remind
    ;;
  rules-gate.sh)
    exec bash "$ROOT/scripts/dev/cursor-hook-bridge.sh" \
      "$ROOT/scripts/dev/claude-rules-gate.py" gate
    ;;
  concurrency-claim.sh)
    exec bash "$ROOT/scripts/dev/cursor-hook-bridge.sh" \
      "$ROOT/scripts/dev/deneb-concurrency-guard.py" claim
    ;;
  *)
    exit 0
    ;;
esac
