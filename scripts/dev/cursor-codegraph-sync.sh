#!/usr/bin/env bash
# Cursor afterFileEdit / postToolUse: background codegraph sync after edits.
# Prefer CURSOR_WORKTREE so the index under the session worktree stays fresh.
set -uo pipefail

cat >/dev/null || true

ROOT="${CURSOR_WORKTREE:-${DENEB_AGENT_ROOT:-${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -d "$ROOT/.codegraph" ]] || exit 0

CG=""
for c in codegraph \
         "$HOME/.local/bin/codegraph" \
         "$HOME/.npm-global/bin/codegraph"; do
  if command -v "$c" >/dev/null 2>&1; then CG="$c"; break; fi
done
[[ -n "$CG" ]] || exit 0

( cd "$ROOT" && bash -lc "$CG sync" >/dev/null 2>&1 ) &
disown 2>/dev/null || true
exit 0
