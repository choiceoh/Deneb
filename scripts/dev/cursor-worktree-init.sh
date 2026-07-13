#!/usr/bin/env bash
# Cursor sessionStart hook: auto-create an isolated git worktree for this chat.
#
# Creates ~/.cursor/worktrees/Deneb/<session-id> on branch cursor/<session-id>,
# seeded from main. Mirrors zcode-worktree-init.sh but uses Cursor-owned paths
# so ZCode/Codex/Claude worktrees never collide.
#
# Cursor's move_agent_to_root can rewrite the worktree branch to main (unsafe),
# so this hook:
#   1. Creates the worktree + seeds CodeGraph
#   2. Injects additional_context + env so the agent scopes Shell/Write there
#   3. Relies on cursor-worktree-guard.sh to block main-checkout edits
#
# Fail-open: any error exits 0 with empty/minimal JSON.
set -uo pipefail

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null || true)
[[ -n "$SESSION_ID" ]] || exit 0

# Sanitize for filesystem / branch names.
SAFE_ID=$(echo "$SESSION_ID" | tr -c 'A-Za-z0-9._-' '_' | cut -c1-80)
[[ -n "$SAFE_ID" ]] || exit 0

ROOT="${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -e "$ROOT/.git" ]] || exit 0

# Already inside a linked worktree? Don't nest.
GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || exit 0
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || exit 0
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || exit 0
if [[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]]; then
  printf '{"env":{"CURSOR_WORKTREE":"%s","DENEB_AGENT_ROOT":"%s"},"additional_context":"Cursor 세션이 이미 링크드 워크트리에 있습니다: %s. 이 경로에서만 편집하세요. CodeGraph MCP(codegraph_explore) 사용 가능."}\n' \
    "$ROOT" "$ROOT" "$ROOT"
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

# Seed CodeGraph index in background (copy + sync).
if [[ -d "$ROOT/.codegraph" ]]; then
  {
    if [[ ! -d "$WT_PATH/.codegraph" ]]; then
      cp -r "$ROOT/.codegraph" "$WT_PATH/.codegraph" 2>/dev/null || true
    fi
    cd "$WT_PATH" && (codegraph sync 2>/dev/null || true)
  } >/dev/null 2>&1 &
  disown 2>/dev/null || true
fi

if [[ "$reuse" -eq 1 ]]; then
  ACTION="재사용"
else
  ACTION="준비됨"
fi

CTX=$(printf '⚡ Cursor 워크트리 %s: %s (브랜치 %s).\n\n이 세션의 모든 편집·셸은 반드시 이 경로를 루트로 사용하세요.\n- Shell: working_directory="%s"\n- Write/StrReplace/Delete/Read: %s 아래 절대경로만\n- CodeGraph: MCP codegraph_explore 우선 (구조·관계·블래스트)\n\n메인 체크아웃(%s, main) 편집은 가드가 차단합니다.\n주의: move_agent_to_root는 워크트리 브랜치를 main으로 덮어쓸 수 있음 → 호출 금지, 경로 스코핑으로 격리.' \
  "$ACTION" "$WT_PATH" "$WT_BRANCH" "$WT_PATH" "$WT_PATH" "$ROOT")

CTX_JSON=$(printf '%s' "$CTX" | jq -Rs .)
printf '{"env":{"CURSOR_WORKTREE":"%s","DENEB_AGENT_ROOT":"%s","CURSOR_SESSION_ID":"%s"},"additional_context":%s}\n' \
  "$WT_PATH" "$WT_PATH" "$SAFE_ID" "$CTX_JSON"
exit 0
