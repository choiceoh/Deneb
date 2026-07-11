#!/bin/bash
# PreToolUse hook (Write|Edit|MultiEdit): block edits in the main checkout.
#
# The SessionStart hook (zcode-worktree-init.sh) creates an isolated worktree
# for this session.  This guard prevents accidentally editing files in the
# main checkout — which would collide with concurrent agents (Codex/Trae/
# Claude) that share the main checkout's working tree.
#
# Behavior:
#   - Inside a linked worktree            → pass (exit 0)
#   - Main checkout, on the main branch   → BLOCK (exit 2) with guidance
#   - Main checkout, on a non-main branch → pass (explicit user choice)
#   - Not a git repo / can't determine    → pass (fail-open)
#
# Exit codes: 0 = pass, 2 = block (PreToolUse deny).
set -uo pipefail

# ── Resolve repo root from hook input ─────────────────────────────────────
INPUT=$(cat)
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
# Find this session's worktree path to guide the agent there.
SESSION_ID="${CLAUDE_SESSION_ID:-}"
WT_HINT=""
if [[ -n "$SESSION_ID" ]]; then
    WT_PATH="$HOME/.zcode/worktrees/Deneb/$SESSION_ID"
    if [[ -d "$WT_PATH" ]]; then
        WT_HINT=$'\n\n→ 워크트리로 이동: cd '"$WT_PATH"
    else
        WT_HINT=$'\n\n→ 워크트리가 아직 없습니다. SessionStart 훅(zcode-worktree-init.sh)을 확인하거나 수동 생성: git worktree add -b zcode/'"$SESSION_ID"' '"$WT_PATH"' main'
    fi
fi

cat >&2 <<EOF
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🛑 메인 체크아웃 편집 차단 (zcode-worktree-guard)

현재 메인 체크아웃(main 브랜치)에서 편집하려 했습니다.
동시 작업 에이전트(Codex/Trae/Claude)와 충돌 위험이 있으므로
전용 워크트리에서 작업해야 합니다.${WT_HINT}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

# Exit 2 = PreToolUse deny (block the tool call).
exit 2
