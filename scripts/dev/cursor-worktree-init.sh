#!/usr/bin/env bash
# Cursor sessionStart hook: auto-create an isolated git worktree for this chat.
#
# Creates ~/.cursor/worktrees/Deneb/<session-id> on branch cursor/<session-id>,
# seeded from main. Mirrors zcode-worktree-init.sh but uses Cursor-owned paths
# so ZCode/Codex/Claude worktrees never collide.
#
# Operational entry (CodeGraph MCP rebind):
#   move_agent_to_root can rewrite the worktree branch to main. After moving,
#   immediately `git checkout cursor/<session-id>` in the worktree. That both
#   restores the branch and rebinds Cursor MCP roots to the worktree so
#   codegraph_explore indexes the session tree (not main).
#
# Also writes ~/.cursor/worktrees/Deneb/active-root for cursor-codegraph-serve.sh.
#
# Fail-open: any error exits 0 with empty/minimal JSON.
set -uo pipefail

emit_json() {
  # Usage: emit_json <wt_path> <session_id> <additional_context>
  jq -nc \
    --arg wt "$1" \
    --arg sid "$2" \
    --arg ctx "$3" \
    '{
      env: {
        CURSOR_WORKTREE: $wt,
        DENEB_AGENT_ROOT: $wt,
        CURSOR_SESSION_ID: $sid
      },
      additional_context: $ctx
    }'
}

write_active_root() {
  local wt="$1"
  local sid="${2:-}"
  local base="$HOME/.cursor/worktrees/Deneb"
  mkdir -p "$base"
  # Session-scoped pin first so concurrent Cursor chats do not clobber each
  # other via a single global active-root (bot review #3604).
  if [[ -n "$sid" ]]; then
    printf '%s\n' "$wt" >"$base/active-root.$sid.tmp" \
      && mv -f "$base/active-root.$sid.tmp" "$base/active-root.$sid"
  fi
  # Global fallback for tools that only know the last writer (codegraph serve).
  printf '%s\n' "$wt" >"$base/active-root.tmp" && mv -f "$base/active-root.tmp" "$base/active-root"
}

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null || true)
[[ -n "$SESSION_ID" ]] || exit 0

SAFE_ID=$(echo "$SESSION_ID" | tr -c 'A-Za-z0-9._-' '_' | cut -c1-80)
[[ -n "$SAFE_ID" ]] || exit 0

ROOT="${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -e "$ROOT/.git" ]] || exit 0

GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || exit 0
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || exit 0
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || exit 0

restore_codegraph_mcp() {
  python3 "$ROOT/scripts/dev/codegraph_mcp_restore.py" --root "$1" >/dev/null 2>&1 || true
}

# Already inside a linked worktree? Don't nest — pin active-root and remind.
if [[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]]; then
  restore_codegraph_mcp "$ROOT"
  write_active_root "$ROOT" "$SAFE_ID"
  BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
  # move_agent_to_root may flip the worktree onto main — never hint "checkout main".
  RECOVER_BRANCH="$BRANCH"
  if [[ "$BRANCH" == "main" || "$BRANCH" == "HEAD" || "$BRANCH" == "unknown" ]]; then
    RECOVER_BRANCH="cursor/$SAFE_ID"
  fi
  CTX=$(printf 'Cursor 세션이 이미 링크드 워크트리에 있습니다: %s (브랜치 %s).\n이 경로에서만 편집하세요. CodeGraph MCP(codegraph_explore) 사용 가능.\n브랜치가 실수로 main이면: git checkout %s' \
    "$ROOT" "$BRANCH" "$RECOVER_BRANCH")
  emit_json "$ROOT" "$SAFE_ID" "$CTX"
  exit 0
fi

# Only auto-create from main (explicit non-main checkout = user choice).
BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0
[[ "$BRANCH" == "main" ]] || exit 0

WT_BASE="$HOME/.cursor/worktrees/Deneb"
WT_PATH="$WT_BASE/$SAFE_ID"
WT_BRANCH="cursor/$SAFE_ID"
mkdir -p "$WT_BASE"

reuse=0
if git -C "$ROOT" worktree list --porcelain 2>/dev/null | grep -q "^worktree ${WT_PATH}$"; then
  reuse=1
else
  if git -C "$ROOT" show-ref --verify --quiet "refs/heads/$WT_BRANCH" 2>/dev/null; then
    git -C "$ROOT" worktree add "$WT_PATH" "$WT_BRANCH" >/dev/null 2>&1 || exit 0
  else
    git -C "$ROOT" worktree add -b "$WT_BRANCH" "$WT_PATH" main >/dev/null 2>&1 || exit 0
  fi
fi

write_active_root "$WT_PATH" "$SAFE_ID"
restore_codegraph_mcp "$ROOT"
restore_codegraph_mcp "$WT_PATH"

# Seed CodeGraph index in background (copy + sync + rpcmap). Skip daemon
# runtime files — they point at the donor's socket and would bind the
# worktree to the wrong process.
if [[ -d "$ROOT/.codegraph" ]]; then
  {
    bash "$ROOT/scripts/dev/codegraph-seed-index.sh" "$ROOT/.codegraph" "$WT_PATH"
  } >/dev/null 2>&1 &
  disown 2>/dev/null || true
fi

if [[ "$reuse" -eq 1 ]]; then
  ACTION="재사용"
else
  ACTION="준비됨"
fi

CTX=$(printf '⚡ Cursor 워크트리 %s: %s (브랜치 %s).\n\n첫 응답에서 아래를 순서대로 실행하세요 (CodeGraph MCP를 워크트리에 재바인딩):\n1) CallMcpTool cursor-app-control / move_agent_to_root  rootPath=%s\n2) Shell (working_directory=%s): git checkout %s\n   (move_agent_to_root가 브랜치를 main으로 덮을 수 있음 → 즉시 복구)\n3) 이후 모든 Write/Shell은 이 워크트리 루트만 사용\n\n메인 체크아웃(%s, main) 편집은 가드가 차단합니다.\nCodeGraph: MCP codegraph_explore 우선 (구조·관계·블래스트).' \
  "$ACTION" "$WT_PATH" "$WT_BRANCH" "$WT_PATH" "$WT_PATH" "$WT_BRANCH" "$ROOT")

emit_json "$WT_PATH" "$SAFE_ID" "$CTX"
exit 0
