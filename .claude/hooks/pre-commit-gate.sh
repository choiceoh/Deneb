#!/bin/bash
# Pre-commit gate: runs format/lint/test checks before scripts/committer.
# Runs on PreToolUse:Bash hook event.
set -euo pipefail

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
TOOL_INPUT=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

# Guard: only intercept scripts/committer calls
[[ "$TOOL_NAME" == "Bash" ]] || exit 0
[[ "$TOOL_INPUT" == scripts/committer* ]] || exit 0

# Find repo root (works in worktrees too)
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
cd "$REPO_ROOT"

# Determine which checks to run based on staged/changed files
CHANGED_FILES=$(git diff --cached --name-only 2>/dev/null; git diff --name-only 2>/dev/null)

TARGETS=""

if echo "$CHANGED_FILES" | grep -q '^core-rs/'; then
  TARGETS="$TARGETS rust-fmt rust-clippy"
fi
if echo "$CHANGED_FILES" | grep -q '^cli-rs/'; then
  TARGETS="$TARGETS cli-fmt cli-clippy"
fi
if echo "$CHANGED_FILES" | grep -q '^gateway-go/'; then
  TARGETS="$TARGETS go-fmt go-vet"
fi

# Nothing to check
[[ -n "$TARGETS" ]] || exit 0

if ! make $TARGETS 2>&1; then
  echo "---"
  echo "Pre-commit gate FAILED. Fix the issues above before committing."
  echo "---"
  exit 2
fi
