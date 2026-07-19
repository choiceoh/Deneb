#!/bin/bash
# PostToolUse hook (Write|Edit|MultiEdit): background codegraph sync after edits.
#
# The file-watcher daemon is active in CodeGraph 1.4.1+, but this hook remains
# as a belt-and-suspenders sync to guarantee the index reflects the latest
# edit.  It runs `codegraph sync` in the background (<0.5s, detached — never
# blocks the session).
#
# Root resolution: the edited file's path (from tool_input.file_path) is the
# primary signal — under ZCode the cwd/CLAUDE_PROJECT_DIR points at the main
# checkout, so using it would sync the WRONG index (main's, not the
# worktree's).  We walk up from the file to find the .codegraph root.
#
# Fail-open: any error exits 0 so tool use is never blocked.
set -uo pipefail

# ── Resolve root from the edited file path ────────────────────────────────
INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // .tool_input.notebook_path // empty' 2>/dev/null)

ROOT=""
if [[ -n "$FILE_PATH" ]]; then
    # Normalize relative paths: the hook cwd is often the main checkout, so
    # a relative path from a worktree context would resolve wrong.  Resolve
    # against the project dir first, then canonicalize.
    ABS_PATH="$FILE_PATH"
    if [[ "$FILE_PATH" != /* ]]; then
        BASE_DIR="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
        ABS_PATH="$BASE_DIR/$FILE_PATH"
    fi
    # Walk up from the file to find the enclosing .codegraph directory.
    DIR=$(cd "$(dirname "$ABS_PATH")" 2>/dev/null && pwd) || DIR=""
    while [[ -n "$DIR" ]] && [[ "$DIR" != "/" ]]; do
        if [[ -d "$DIR/.codegraph" ]]; then
            ROOT="$DIR"
            break
        fi
        DIR=$(dirname "$DIR")
    done
fi
# Fallback: project dir env (main checkout — last resort, may be wrong root).
if [[ -z "$ROOT" ]]; then
    ROOT="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
    ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
fi
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
# After the sync, re-inject rpcmap's string-keyed dispatch edges (synthetic
# rows survive incremental syncs, but a rebuild drops them; re-running is
# idempotent and <1s, so just always chase the sync with it).
# The semantic embedding index (make codesearch-index) rides along when one
# exists: incremental (only changed nodes re-embed), debounced to 120s so
# rapid edit bursts cost one refresh, and gated on a 1s embedder health probe
# so machines without the sidecar skip silently.
( cd "$ROOT" && bash -lc "$CG sync" >/dev/null 2>&1; \
  python3 "$ROOT/scripts/dev/rpcmap_codegraph_sync.py" >/dev/null 2>&1; \
  SEM="$ROOT/.codegraph/semantic-code.json"; \
  STAMP="$ROOT/.codegraph/.semantic-sync-stamp"; \
  if [[ -f "$SEM" && -d "$ROOT/gateway-go" ]]; then \
      now=$(date +%s); last=0; \
      [[ -f "$STAMP" ]] && last=$(cat "$STAMP" 2>/dev/null || echo 0); \
      if (( now - last >= 120 )) && \
         curl -sf -m 1 "${DENEB_EMBEDDING_URL:-http://127.0.0.1:8002}/health" >/dev/null 2>&1; then \
          echo "$now" > "$STAMP"; \
          (cd "$ROOT/gateway-go" && bash -lc "go run ./cmd/codesearch index") >/dev/null 2>&1; \
      fi; \
  fi ) &
disown 2>/dev/null || true

exit 0
