#!/bin/bash
# SessionStart hook: auto-create a ZCode-dedicated worktree for isolation.
#
# Creates ~/.zcode/worktrees/Deneb/<session-id> on branch zcode/<session-id>,
# seeded from main.  This prevents concurrent agents (Codex/Trae/Claude) from
# colliding in the main checkout.  Mirrors the Claude Code worktree pattern
# (see .claude/hooks/worktree-auto-pr.sh, scripts/dev/codegraph-autoindex.py).
#
# Design:
#   - Idempotent: reuses an existing worktree for the same session.
#   - Fail-open: any error exits 0 so the session is never blocked.
#   - Only fires from the main checkout on the main branch (never nests
#     worktrees or interferes with an explicitly-checked-out branch).
#   - CodeGraph index is copied from the main checkout and synced in the
#     background (same cheap-copy strategy as codegraph-autoindex.py).
set -uo pipefail

# ── Resolve session ID ────────────────────────────────────────────────────
# ZCode injects CLAUDE_SESSION_ID as an env var; fall back to stdin JSON.
INPUT=$(cat)
SESSION_ID="${CLAUDE_SESSION_ID:-}"
if [[ -z "$SESSION_ID" ]]; then
    SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty' 2>/dev/null)
fi
# No session ID → can't create a named worktree. Fail-open.
[[ -n "$SESSION_ID" ]] || exit 0

# ── Resolve main checkout root ────────────────────────────────────────────
ROOT="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || exit 0
# Must be a git repo with a .git entry.
[[ -e "$ROOT/.git" ]] || exit 0

# ── Guard: already inside a worktree? ─────────────────────────────────────
# In the main checkout, --git-dir and --git-common-dir resolve to the same
# path.  In a linked worktree they differ.  If we're already in a worktree,
# don't nest — pin CodeGraph and exit.
GIT_DIR=$(cd "$ROOT" && git rev-parse --git-dir 2>/dev/null) || exit 0
GIT_COMMON=$(cd "$ROOT" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
GIT_DIR_ABS=$(cd "$ROOT" && cd "$GIT_DIR" 2>/dev/null && pwd) || exit 0
GIT_COMMON_ABS=$(cd "$ROOT" && cd "$GIT_COMMON" 2>/dev/null && pwd) || exit 0

pin_codegraph() {
    python3 "$ROOT/scripts/dev/codegraph_root.py" write-active --agent zcode --path "$1" --sid "$SESSION_ID" >/dev/null 2>&1 || true
    python3 "$ROOT/scripts/dev/codegraph_mcp_restore.py" --root "$1" --no-user >/dev/null 2>&1 || true
}

# Already inside a linked worktree? Don't nest — pin CodeGraph and continue.
if [[ "$GIT_DIR_ABS" != "$GIT_COMMON_ABS" ]]; then
    pin_codegraph "$ROOT"
    exit 0
fi

# ── Guard: only auto-create from main ─────────────────────────────────────
# If the user explicitly checked out another branch in the main checkout,
# respect that (CLAUDE.md:18 — branch switching is explicit-only).
BRANCH=$(cd "$ROOT" && git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0
[[ "$BRANCH" == "main" ]] || exit 0

# ── Worktree paths ────────────────────────────────────────────────────────
WT_BASE="$HOME/.zcode/worktrees/Deneb"
WT_PATH="$WT_BASE/$SESSION_ID"
WT_BRANCH="zcode/$SESSION_ID"

mkdir -p "$WT_BASE"

# ── Idempotent: reuse if the worktree already exists ──────────────────────
# Prune stale worktree registrations first — a half-deleted worktree (dir
# gone but registration remains) would cause `git worktree add` to fail
# silently under the `|| exit 0` guard below.
git worktree prune 2>/dev/null || true
if git worktree list --porcelain 2>/dev/null | grep -q "^worktree ${WT_PATH}$"; then
    pin_codegraph "$WT_PATH"
    printf '{"additionalContext":"⚡ ZCode 워크트리 재사용: %s (브랜치 %s).\\n\\n첫 작업 전 반드시 진입: cd %s\\n이 디렉터리에서만 편집하세요 — 메인 체크아웃(%s)에서의 Write/Edit/MultiEdit는 가드가 차단합니다."}\n' \
        "$WT_PATH" "$WT_BRANCH" "$WT_PATH" "$ROOT"
    exit 0
fi

# ── Create the worktree ───────────────────────────────────────────────────
# Branch from origin/main (not local main) so sessions start from upstream
# HEAD even when the user hasn't pulled.  A quiet fetch first; if it fails
# (offline), fall back to local main.
git fetch --quiet origin main 2>/dev/null || true
SEED_REF="main"
if git rev-parse --verify origin/main >/dev/null 2>&1; then
    SEED_REF="origin/main"
fi
if git show-ref --verify --quiet "refs/heads/$WT_BRANCH" 2>/dev/null; then
    # Branch exists (worktree was cleaned up earlier) — re-attach.
    git worktree add "$WT_PATH" "$WT_BRANCH" >&2 2>/dev/null || exit 0
else
    # Fresh branch from origin/main (or local main if fetch failed).
    git worktree add -b "$WT_BRANCH" "$WT_PATH" "$SEED_REF" >&2 2>/dev/null || exit 0
fi

# ── Seed CodeGraph index (background, fail-open) ──────────────────────────
# Copy the main checkout's index, then run `codegraph sync` to reconcile
# branch drift.  Daemon runtime files (pid/sock/log) are excluded — they
# point at the main checkout's socket and would confuse the worktree's
# codegraph into talking to the wrong daemon.  Runs detached so session
# start is never delayed.
if [[ -d "$ROOT/.codegraph" ]]; then
    {
        bash "$ROOT/scripts/dev/codegraph-seed-index.sh" "$ROOT/.codegraph" "$WT_PATH"
    } >/dev/null 2>&1 &
    disown 2>/dev/null || true
fi
pin_codegraph "$WT_PATH"

# ── Report to agent ───────────────────────────────────────────────────────
# Strong directive: the agent must cd into the worktree before any edit,
# otherwise the guard (zcode-worktree-guard.sh) blocks Write/Edit/MultiEdit
# in the main checkout.
printf '{"additionalContext":"⚡ ZCode 워크트리 준비됨: %s (브랜치 %s).\\n\\n첫 작업 전 반드시 진입: cd %s\\n이 디렉터리에서만 편집하세요 — 메인 체크아웃(%s)에서의 Write/Edit/MultiEdit는 가드가 차단합니다."}\n' \
    "$WT_PATH" "$WT_BRANCH" "$WT_PATH" "$ROOT"
exit 0
