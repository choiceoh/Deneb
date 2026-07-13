#!/usr/bin/env bash
# Cursor preToolUse (Write|StrReplace|Delete|EditNotebook): block main-checkout edits.
#
# Allows target paths under:
#   - ~/.cursor/worktrees/Deneb/*
#   - $CURSOR_WORKTREE (session pin)
#   - the current linked worktree root (when cwd is already a worktree)
# Denies:
#   - writes into the primary checkout while on main
#   - absolute paths that escape a linked worktree back to the primary checkout
#   - path-traversal attempts (`..`) that fail realpath normalization
set -uo pipefail

deny() {
  local msg="$1"
  local hint="$2"
  local full
  full=$(printf '%s\n%s' "$msg" "$hint")
  # failClosed hooks require valid JSON on stdout even when jq is missing
  # (bot review #3604).
  if command -v jq >/dev/null 2>&1; then
    jq -nc --arg m "$full" \
      '{permission:"deny", user_message:"메인 체크아웃 편집이 차단되었습니다. Cursor 워크트리를 사용하세요.", agent_message:$m}' \
      2>/dev/null && exit 0
  fi
  python3 -c 'import json,sys; print(json.dumps({"permission":"deny","user_message":"메인 체크아웃 편집이 차단되었습니다. Cursor 워크트리를 사용하세요.","agent_message":sys.argv[1]}, ensure_ascii=False))' "$full" 2>/dev/null && exit 0
  # Last resort: static JSON (message truncated) so failClosed never sees empty stdout.
  printf '%s\n' '{"permission":"deny","user_message":"메인 체크아웃 편집이 차단되었습니다. Cursor 워크트리를 사용하세요.","agent_message":"main checkout edit blocked"}'
  exit 0
}

allow() {
  # failClosed hooks treat empty stdout as failure — always emit allow JSON.
  printf '%s\n' '{"permission":"allow"}'
  exit 0
}

resolve_abs() {
  # Echo realpath or empty on failure. Reject raw `..` segments before resolve
  # so a missing python/realpath fallback cannot prefix-bypass WT_BASE.
  local raw="$1"
  if [[ "$raw" == *'..'* ]]; then
    # Explicit deny for traversal segments (bot #3604: prior case was a no-op).
    return 1
  fi
  if command -v realpath >/dev/null 2>&1; then
    realpath -m "$raw" 2>/dev/null || python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$raw" 2>/dev/null || return 1
  else
    python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$raw" 2>/dev/null || return 1
  fi
}

under() {
  # True if $1 is $2 or a path under $2.
  local path="$1" base="$2"
  [[ -n "$base" ]] || return 1
  [[ "$path" == "$base" || "$path" == "$base"/* ]]
}

INPUT=$(cat)
ROOT="${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || allow
[[ -e "$ROOT/.git" ]] || allow

TARGET=$(echo "$INPUT" | jq -r '
  .tool_input.path // .tool_input.file_path // .tool_input.target_notebook // empty
' 2>/dev/null || true)

GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || allow
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || allow
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || allow
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || allow
IS_LINKED=0
[[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]] && IS_LINKED=1

# Primary checkout root (parent of .git common dir).
PRIMARY="$ROOT"
if [[ "$IS_LINKED" -eq 1 ]]; then
  PRIMARY=$(cd "$GIT_COMMON_ABS/.." 2>/dev/null && pwd) || PRIMARY="$ROOT"
fi

BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null) || allow
WT_BASE="$HOME/.cursor/worktrees/Deneb"

SESSION_ID="${CURSOR_SESSION_ID:-}"
if [[ -z "$SESSION_ID" ]]; then
  SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null || true)
fi
SAFE_SID=""
if [[ -n "$SESSION_ID" ]]; then
  SAFE_SID=$(printf '%s' "$SESSION_ID" | tr -c 'A-Za-z0-9._-' '_' | cut -c1-80)
fi
WT_PATH=""
if [[ -n "${CURSOR_WORKTREE:-}" && -d "${CURSOR_WORKTREE}" ]]; then
  WT_PATH="$CURSOR_WORKTREE"
elif [[ -n "$SAFE_SID" && -d "$WT_BASE/$SAFE_SID" ]]; then
  WT_PATH="$WT_BASE/$SAFE_SID"
elif [[ -n "$SAFE_SID" && -f "$WT_BASE/active-root.$SAFE_SID" ]]; then
  WT_PATH=$(head -1 "$WT_BASE/active-root.$SAFE_SID" 2>/dev/null || true)
elif [[ -f "$WT_BASE/active-root" ]]; then
  WT_PATH=$(head -1 "$WT_BASE/active-root" 2>/dev/null || true)
fi
if [[ -n "$WT_PATH" && -d "$WT_PATH" ]]; then
  HINT=$(printf '→ 워크트리 경로로 재시도:\n  path/working_directory: %s' "$WT_PATH")
else
  HINT='→ 워크트리 없음. 생성:\n  bash scripts/dev/cursor-worktree-init.sh'
fi
DENY_MSG='메인 체크아웃 편집 차단 (cursor-worktree-guard). 동시 에이전트 충돌 방지를 위해 Cursor 전용 워크트리에서만 편집하세요.'

# No target path: only allow when already inside a linked worktree (cwd scoped).
if [[ -z "$TARGET" ]]; then
  if [[ "$IS_LINKED" -eq 1 ]]; then
    allow
  fi
  if [[ "$BRANCH" != "main" ]]; then
    allow  # explicit non-main checkout in primary
  fi
  deny "$DENY_MSG" "$HINT"
fi

if [[ "$TARGET" = /* ]]; then
  TARGET_RAW="$TARGET"
else
  TARGET_RAW="$ROOT/$TARGET"
fi

TARGET_ABS=$(resolve_abs "$TARGET_RAW") || deny "$DENY_MSG (경로 정규화 실패 — '..' 우회 의)" "$HINT"

# Always allow Cursor session worktrees.
if under "$TARGET_ABS" "$WT_BASE"; then
  allow
fi
if [[ -n "$WT_PATH" ]] && under "$TARGET_ABS" "$WT_PATH"; then
  allow
fi

# Linked worktree cwd: allow only targets inside this worktree root.
if [[ "$IS_LINKED" -eq 1 ]]; then
  if under "$TARGET_ABS" "$ROOT"; then
    allow
  fi
  deny "$DENY_MSG (링크드 워크트리에서 외부/메인 경로 편집 차단)" "$HINT"
fi

# Primary checkout on a non-main branch = explicit user choice.
if [[ "$BRANCH" != "main" ]]; then
  allow
fi

# Primary checkout on main: deny (target was not under a Cursor worktree).
deny "$DENY_MSG" "$HINT"
