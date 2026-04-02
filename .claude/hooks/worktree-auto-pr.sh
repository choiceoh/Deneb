#!/bin/bash
# Auto-push and create PR when exiting a Claude Code worktree.
# Runs on PostToolUse:ExitWorktree hook event.
set -euo pipefail

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')

# Guard: only run for ExitWorktree
[[ "$TOOL_NAME" == "ExitWorktree" ]] || exit 0

# Get worktree path from tool input
WORKTREE_PATH=$(echo "$INPUT" | jq -r '.tool_input.worktree_path // empty')
[[ -n "$WORKTREE_PATH" && -d "$WORKTREE_PATH" ]] || exit 0

cd "$WORKTREE_PATH" || exit 0

BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0

# Skip main
[[ "$BRANCH" != "main" ]] || exit 0

# Check if there are any commits ahead of main
AHEAD=$(git rev-list --count main.."$BRANCH" 2>/dev/null) || exit 0
[[ "$AHEAD" -gt 0 ]] || exit 0

# Push
git push -u origin "$BRANCH" 2>&1 || true

# Create PR if none exists
if ! gh pr view "$BRANCH" --json number &>/dev/null; then
  gh pr create \
    --base main \
    --head "$BRANCH" \
    --fill \
    2>&1 || true
fi
