#!/bin/bash
# zcode-push.sh — auto-fetch + rebase + push in one step.
#
# Solves the multi-agent push collision: when Codex/Trae/Claude push to main
# between our commits, `git push` fails with non-fast-forward.  This wrapper
# automates the manual `fetch + rebase + push` cycle we keep repeating.
#
# Usage:
#   zcode-push.sh                  # push current branch to origin
#   zcode-push.sh main             # push to origin/main explicitly
#   zcode-push.sh --force          # force push (use with care)
#
# Safety:
#   - Never pushes if rebase fails (conflicts left for manual resolution).
#   - Refuses to force-push main.
#   - Stashes uncommitted changes before rebase, restores after.
set -euo pipefail

BRANCH="${1:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '')}"
FORCE=""
[[ "$BRANCH" == "--force" ]] && { FORCE="--force-with-lease"; BRANCH="${2:-$(git rev-parse --abbrev-ref HEAD)}"; }

if [[ -z "$BRANCH" ]]; then
    echo "Error: cannot determine branch. Usage: zcode-push.sh [branch] [--force]" >&2
    exit 1
fi

# Refuse to force-push main (protection).
if [[ "$FORCE" != "" && "$BRANCH" == "main" ]]; then
    echo "Error: refusing to force-push main." >&2
    exit 1
fi

echo "→ Fetching origin..."
git fetch origin

# Check if remote has new commits.
REMOTE_REF="origin/$BRANCH"
AHEAD=$(git rev-list --count HEAD.."$REMOTE_REF" 2>/dev/null || echo 0)

if [[ "$AHEAD" -gt 0 ]]; then
    echo "→ Remote is $AHEAD commit(s) ahead — rebasing..."

    # Stash uncommitted changes if any.
    STASHED=false
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "  Stashing uncommitted changes..."
        git stash >/dev/null 2>&1
        STASHED=true
    fi

    # Rebase.  If it fails, abort and exit with error.
    if ! git rebase "$REMOTE_REF"; then
        echo "Error: rebase failed — conflicts need manual resolution." >&2
        echo "  Run 'git rebase --abort' to cancel, then resolve manually." >&2
        if $STASHED; then
            echo "  (stashed changes are preserved — run 'git stash pop' after resolving)" >&2
        fi
        exit 1
    fi

    # Restore stashed changes.
    if $STASHED; then
        echo "  Restoring stashed changes..."
        git stash pop >/dev/null 2>&1 || echo "  Warning: stash pop had conflicts — run 'git stash pop' manually." >&2
    fi
    echo "  Rebase complete."
else
    echo "  Up to date with remote."
fi

echo "→ Pushing to origin/$BRANCH..."
if [[ -n "$FORCE" ]]; then
    git push $FORCE origin "$BRANCH"
else
    git push origin "$BRANCH"
fi

echo "✅ Pushed $BRANCH to origin."
