#!/bin/bash
# zcode-cleanup.sh — remove stale/merged zcode worktrees and branches.
#
# ZCode sessions create worktrees under ~/.zcode/worktrees/Deneb/<session-id>.
# Over time, completed sessions leave orphaned worktrees and branches that
# consume disk space and clutter `git worktree list`.  This script:
#
#   1. Removes worktrees whose branch has been merged into main.
#   2. Removes worktrees older than N days (default: 7) with no uncommitted
#      changes (safe — dirty worktrees are preserved).
#   3. Deletes the corresponding zcode/* branches after worktree removal.
#
# Usage:
#   zcode-cleanup.sh              # dry-run — show what would be removed
#   zcode-cleanup.sh --apply      # actually remove
#   zcode-cleanup.sh --apply 14   # custom age threshold (days)
set -uo pipefail

APPLY=false
MAX_AGE_DAYS=7

while [[ $# -gt 0 ]]; do
    case "$1" in
        --apply) APPLY=true; shift ;;
        --apply*) APPLY=true; shift; [[ $# -gt 0 ]] && MAX_AGE_DAYS="$1" && shift ;;
        *) MAX_AGE_DAYS="$1"; shift ;;
    esac
done

ROOT="${CLAUDE_PROJECT_DIR:-${ZCODE_PROJECT_DIR:-$(pwd)}}"
cd "$ROOT" 2>/dev/null || exit 1

WT_BASE="$HOME/.zcode/worktrees/Deneb"
[[ -d "$WT_BASE" ]] || { echo "zcode 워크트리 디렉터리 없음 — 정리 불필요."; exit 0; }

NOW=$(date +%s)
CUTOFF=$((NOW - MAX_AGE_DAYS * 86400))

MERGED_BRANCHES=$(git branch --merged main 2>/dev/null | grep 'zcode/' | sed 's/[* ]*//' || true)

REMOVED=0
KEPT=0

MODE="시뮬레이션"
$APPLY && MODE="실행"

echo "ZCode 워크트리 정리 (${MODE}, 임계=${MAX_AGE_DAYS}일)"
echo "─────────────────────────────────────────────────"

for wt_dir in "$WT_BASE"/*/; do
    [[ -d "$wt_dir" ]] || continue
    wt_path="${wt_dir%/}"
    wt_name=$(basename "$wt_path")

    # Check if this is a registered worktree.
    if ! git worktree list --porcelain 2>/dev/null | grep -q "^worktree ${wt_path}$"; then
        # Orphaned directory (worktree already pruned but dir remains).
        if [[ "$APPLY" == true ]]; then
            rm -rf "$wt_path"
            echo "  🗑️  제거 (orphan dir): $wt_name"
        else
            echo "  🔍 제거 대상 (orphan dir): $wt_name"
        fi
        ((REMOVED++))
        continue
    fi

    # Get branch name.
    branch=$(cd "$wt_path" && git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    [[ -z "$branch" ]] && continue

    # Skip if it has uncommitted changes.
    if ! (cd "$wt_path" && git diff --quiet 2>/dev/null) || ! (cd "$wt_path" && git diff --cached --quiet 2>/dev/null); then
        echo "  🔒 유지 (미커밋 변경): $wt_name ($branch)"
        ((KEPT++))
        continue
    fi

    # Check if merged into main.
    is_merged=false
    if echo "$MERGED_BRANCHES" | grep -qx "$branch" 2>/dev/null; then
        is_merged=true
    fi

    # Check age (directory mtime as proxy for last activity).
    wt_mtime=$(stat -f %m "$wt_path" 2>/dev/null || stat -c %Y "$wt_path" 2>/dev/null || echo 0)
    is_old=false
    [[ "$wt_mtime" -lt "$CUTOFF" ]] && is_old=true

    if $is_merged; then
        reason="merged into main"
    elif $is_old; then
        reason="older than ${MAX_AGE_DAYS}d, no changes"
    else
        [[ "$APPLY" != true ]] && echo "  ✅ 유지 (활성): $wt_name ($branch)"
        ((KEPT++))
        continue
    fi

    if [[ "$APPLY" == true ]]; then
        git worktree remove --force "$wt_path" 2>/dev/null || rm -rf "$wt_path"
        git branch -D "$branch" 2>/dev/null || true
        echo "  🗑️  제거 ($reason): $wt_name ($branch)"
    else
        echo "  🔍 제거 대상 ($reason): $wt_name ($branch)"
    fi
    ((REMOVED++))
done

echo "─────────────────────────────────────────────────"
echo "결과: ${REMOVED}개 제거 (${MODE}), ${KEPT}개 유지"
[[ "$APPLY" != true ]] && echo "실행하려면: zcode-cleanup.sh --apply"
