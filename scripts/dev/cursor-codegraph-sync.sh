#!/usr/bin/env bash
# Cursor afterFileEdit / postToolUse: background codegraph refresh after edits.
# Prefer CURSOR_WORKTREE so the index under the session worktree stays fresh.
set -uo pipefail

cat >/dev/null || true

ROOT="${CURSOR_WORKTREE:-${DENEB_AGENT_ROOT:-${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -d "$ROOT/.codegraph" ]] || exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
( bash "$HERE/codegraph-sync-run.sh" "$ROOT" ) >/dev/null 2>&1 &
disown 2>/dev/null || true
exit 0
