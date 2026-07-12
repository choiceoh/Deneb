#!/bin/bash
# PostToolUse hook (Write|Edit|MultiEdit): background codegraph sync after edits.
#
# CodeGraph's file-watcher daemon is inactive in some versions (1.4.x), so the
# index can drift from the working tree after edits.  This hook runs
# `codegraph sync` in the background after each Write/Edit/MultiEdit so the
# index stays fresh (<0.5s, detached — never blocks the session).
#
# Fail-open: any error exits 0 so tool use is never blocked.
set -uo pipefail

# ── Resolve repo root ─────────────────────────────────────────────────────
ROOT="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -d "$ROOT/.codegraph" ]] || exit 0  # no index → nothing to sync

# ── Find codegraph binary ─────────────────────────────────────────────────
CG=""
for c in codegraph \
         "$HOME/.local/bin/codegraph" \
         "$HOME/.npm-global/bin/codegraph"; do
    if command -v "$c" >/dev/null 2>&1; then CG="$c"; break; fi
done
[[ -n "$CG" ]] || exit 0  # not installed → skip

# ── Background sync (detached, fire-and-forget) ───────────────────────────
# Login shell so the profile PATH (node + codegraph) is restored.  Detached
# so the session continues immediately — the index updates a moment later.
( cd "$ROOT" && bash -lc "$CG sync" >/dev/null 2>&1 ) &
disown 2>/dev/null || true

exit 0
