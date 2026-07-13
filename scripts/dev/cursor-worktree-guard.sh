#!/usr/bin/env bash
# Cursor preToolUse (Write|StrReplace|Delete|EditNotebook): block main-checkout edits.
#
# Allows:
#   - edits whose path is under ~/.cursor/worktrees/Deneb/
#   - any edit when the workspace root is already a linked worktree
#   - main-checkout edits on a non-main branch (explicit user choice)
# Denies:
#   - main-checkout on main, targeting a path outside the Cursor worktree
set -uo pipefail

INPUT=$(cat)
ROOT="${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -e "$ROOT/.git" ]] || exit 0

TARGET=$(echo "$INPUT" | jq -r '
  .tool_input.path // .tool_input.file_path // .tool_input.target_notebook // empty
' 2>/dev/null || true)

# Already in a linked worktree → pass.
GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || exit 0
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || exit 0
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || exit 0
[[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]] && exit 0

BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0
[[ "$BRANCH" == "main" ]] || exit 0

WT_BASE="$HOME/.cursor/worktrees/Deneb"
if [[ -n "$TARGET" ]]; then
  if [[ "$TARGET" = /* ]]; then
    TARGET_ABS="$TARGET"
  else
    TARGET_ABS="$ROOT/$TARGET"
  fi
  # Normalize .. components when possible.
  TARGET_ABS=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$TARGET_ABS" 2>/dev/null || echo "$TARGET_ABS")
  case "$TARGET_ABS" in
    "$WT_BASE"/*) exit 0 ;;
  esac
  if [[ -n "${CURSOR_WORKTREE:-}" ]]; then
    case "$TARGET_ABS" in
      "$CURSOR_WORKTREE"/*|"$CURSOR_WORKTREE") exit 0 ;;
    esac
  fi
fi

SESSION_ID="${CURSOR_SESSION_ID:-}"
if [[ -z "$SESSION_ID" ]]; then
  SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null || true)
fi
WT_PATH=""
if [[ -n "${CURSOR_WORKTREE:-}" && -d "${CURSOR_WORKTREE}" ]]; then
  WT_PATH="$CURSOR_WORKTREE"
elif [[ -n "$SESSION_ID" && -d "$WT_BASE/$SESSION_ID" ]]; then
  WT_PATH="$WT_BASE/$SESSION_ID"
elif [[ -d "$WT_BASE" ]]; then
  WT_PATH=$(ls -dt "$WT_BASE"/*/ 2>/dev/null | head -1)
  WT_PATH=${WT_PATH%/}
fi

if [[ -n "$WT_PATH" && -d "$WT_PATH" ]]; then
  HINT=$(printf '→ 워크트리 경로로 재시도:\n  path/working_directory: %s' "$WT_PATH")
else
  HINT='→ 워크트리 없음. 생성:\n  bash scripts/dev/cursor-worktree-init.sh'
fi

AGENT_MSG=$(printf '메인 체크아웃(main) 편집 차단 (cursor-worktree-guard).\n동시 에이전트 충돌 방지를 위해 Cursor 전용 워크트리에서만 편집하세요.\n%s' "$HINT")
AGENT_JSON=$(printf '%s' "$AGENT_MSG" | jq -Rs .)

printf '{"permission":"deny","user_message":"메인 체크아웃 편집이 차단되었습니다. Cursor 워크트리를 사용하세요.","agent_message":%s}\n' "$AGENT_JSON"
exit 0
