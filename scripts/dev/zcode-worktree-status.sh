#!/bin/bash
# Stop hook: report worktree status (no PR/push — explicit-only per CLAUDE.md:18).
#
# Summarizes the session's worktree changes as additionalContext so the agent
# and user can see what was done.  Performs no push/PR/cleanup — those remain
# explicit-only operations per the multi-agent safety rule (CLAUDE.md:18).
#
# Fail-open: any error exits 0 so the Stop event is never blocked.
set -uo pipefail

# ── Resolve session ID & worktree path ────────────────────────────────────
INPUT=$(cat)
SESSION_ID="${CLAUDE_SESSION_ID:-}"
[[ -n "$SESSION_ID" ]] || exit 0

WT_PATH="$HOME/.zcode/worktrees/Deneb/$SESSION_ID"
[[ -d "$WT_PATH" ]] || exit 0  # no worktree for this session — nothing to report

cd "$WT_PATH" 2>/dev/null || exit 0

# ── Gather status ─────────────────────────────────────────────────────────
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0

# Commits ahead of main (this session's contribution).
AHEAD=$(git rev-list --count main.."${BRANCH}" 2>/dev/null || echo 0)

# Uncommitted changes (staged + unstaged file count).
CHANGED=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')

# Compose a concise status line.  additionalContext is injected into the
# conversation as system-visible context (strict JSON schema — only
# recognized keys allowed).
if [[ "$AHEAD" -gt 0 || "$CHANGED" -gt 0 ]]; then
    MSG=$(printf "ZCode 워크트리 상태: %s — 커밋 %s개(main 대비), 미커밋 변경 %s건. PR/push는 명시 요청 시에만." \
        "$WT_PATH" "$AHEAD" "$CHANGED")
else
    MSG=$(printf "ZCode 워크트리 상태: %s — 변경사항 없음. 워크트리는 세션 간 유지됩니다." "$WT_PATH")
fi

# Emit JSON with additionalContext (hook output schema).
printf '{"additionalContext":%s}\n' "$(printf '%s' "$MSG" | jq -Rs .)"
exit 0
