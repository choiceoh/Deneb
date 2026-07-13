#!/bin/bash
# PreToolUse hook (Write|Edit|MultiEdit): block edits in the main checkout.
#
# The SessionStart hook (zcode-worktree-init.sh) creates an isolated worktree
# for this session.  This guard prevents accidentally editing files in the
# main checkout — which would collide with concurrent agents (Codex/Trae/
# Claude) that share the main checkout's working tree.
#
# Upgrade history (2026-07-13): the original cwd-only guard blocked every
# worktree edit because the harness resets cwd to the main checkout between
# tool calls. The file-path gate now makes tool_input.file_path the primary
# signal: if the target lives inside any worktree it passes immediately,
# regardless of cwd.
#
# Behavior (file-path aware):
#   - tool_input.file_path inside a linked worktree → pass (exit 0)
#   - tool_input.file_path in the main checkout      → BLOCK if on main (exit 2)
#   - no file_path (defensive)                        → fall back to cwd check
#   - cwd inside a linked worktree                    → pass (exit 0)
#   - cwd main checkout, on the main branch           → BLOCK (exit 2)
#   - cwd main checkout, on a non-main branch         → pass (explicit user choice)
#   - not a git repo / can't determine                → pass (fail-open)
#
# The file-path check is primary: the harness resets cwd to the main checkout
# between tool calls, so a cwd-only guard would block legitimate worktree edits
# every time. Reading tool_input.file_path from the hook payload lets the guard
# pass edits whose target lives inside a worktree regardless of where the shell
# happens to be sitting.
#
# Exit codes: 0 = pass, 2 = block (PreToolUse deny).
set -uo pipefail

# ── Resolve the file path from the hook payload ───────────────────────────
INPUT=$(cat)

# tool_input.file_path (Edit/Write) or tool_input.notebook_path cover every
# path-carrying tool in the matcher. Empty when the payload omits it.
FILE_PATH=$(echo "$INPUT" | jq -r '
  .tool_input.file_path //
  .tool_input.notebook_path //
  empty' 2>/dev/null)

# ── File-path gate (primary): pass if the target is inside any worktree ───
# WT_BASE holds every agent worktree root. A target beneath it is isolated by
# construction, so it can never collide with the main checkout regardless of
# the current cwd.
WT_BASE="$HOME/.zcode/worktrees"
if [[ -n "$FILE_PATH" ]]; then
    case "$(cd "$FILE_PATH" 2>/dev/null && pwd || echo "$FILE_PATH")" in
        "$WT_BASE"/*)
            exit 0
            ;;
    esac
fi

# ── cwd gate (fallback): resolve repo root from hook input ────────────────
# The tool input may carry the file path; cwd is the safest repo indicator.
ROOT="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
[[ -e "$ROOT/.git" ]] || exit 0

# ── Already inside a linked worktree? ─────────────────────────────────────
# In the main checkout, --git-dir and --git-common-dir resolve to the same
# path.  In a linked worktree they differ → we're isolated → pass.
GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || exit 0
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || exit 0
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || exit 0
[[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]] && exit 0

# ── Main checkout: is it on the main branch? ──────────────────────────────
BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0
# Non-main branch in the main checkout = explicit user choice; respect it.
[[ "$BRANCH" == "main" ]] || exit 0

# ── Block: editing main checkout on main branch ───────────────────────────
# Find a zcode worktree to guide the agent to.  CLAUDE_SESSION_ID is the
# first choice, but it may not be injected into the PreToolUse env.  Fall
# back to: stdin JSON session_id → most-recently-created zcode worktree.
SESSION_ID="${CLAUDE_SESSION_ID:-}"
if [[ -z "$SESSION_ID" ]]; then
    # Try stdin payload (ZCode hook input JSON).
    STDIN_SID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null)
    [[ -n "$STDIN_SID" ]] && SESSION_ID="$STDIN_SID"
fi

WT_PROJECT_BASE="$HOME/.zcode/worktrees/Deneb"
WT_PATH=""
if [[ -n "$SESSION_ID" && -d "$WT_PROJECT_BASE/$SESSION_ID" ]]; then
    WT_PATH="$WT_PROJECT_BASE/$SESSION_ID"
elif [[ -d "$WT_PROJECT_BASE" ]]; then
    # Fallback: pick the most recently modified zcode worktree.  ls -t sorts
    # by mtime; head -1 takes the newest.  This is robust even when the
    # session ID is unavailable (e.g., env not propagated to PreToolUse).
    WT_PATH=$(ls -dt "$WT_PROJECT_BASE"/*/ 2>/dev/null | head -1)
    WT_PATH=${WT_PATH%/}  # strip trailing slash
fi

WT_HINT=""
if [[ -n "$WT_PATH" && -d "$WT_PATH" ]]; then
    WT_HINT=$'\n\n→ 즉시 워크트리로 진입 후 같은 편집 재시도:\n  cd '"$WT_PATH"
else
    # No worktree exists at all — likely SessionStart didn't fire or failed.
    WT_HINT=$'\n\n→ 워크트리가 없습니다. 생성 후 진입:\n  bash scripts/dev/zcode-worktree-init.sh'
fi

cat >&2 <<EOF
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🛑 메인 체크아웃 편집 차단 (zcode-worktree-guard)

메인 체크아웃(main 브랜치)에서 편집을 시도했습니다.
동시 작업 에이전트(Codex/Trae/Claude)와 충돌을 막기 위해
전용 워크트리에서만 편집할 수 있습니다.${WT_HINT}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

# Exit 2 = PreToolUse deny (block the tool call).
exit 2
