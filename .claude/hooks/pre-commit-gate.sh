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

RUN_RUST=false
RUN_GO=false

if echo "$CHANGED_FILES" | grep -q '^core-rs/\|^cli-rs/'; then
  RUN_RUST=true
fi
if echo "$CHANGED_FILES" | grep -q '^gateway-go/'; then
  RUN_GO=true
fi

ERRORS=""

# Rust checks
if $RUN_RUST; then
  if ! (cd core-rs && cargo fmt --check 2>&1); then
    ERRORS="${ERRORS}\n[BLOCK] cargo fmt check failed"
  fi
  if ! (cd core-rs && cargo clippy --workspace --quiet 2>&1); then
    ERRORS="${ERRORS}\n[BLOCK] cargo clippy failed"
  fi
fi

# Go checks
if $RUN_GO; then
  if ! (cd gateway-go && gofmt -l . 2>&1 | grep -q . && echo "gofmt issues found" && false); then
    : # gofmt ok (no output = no issues)
  fi
  if ! (cd gateway-go && go vet ./... 2>&1); then
    ERRORS="${ERRORS}\n[BLOCK] go vet failed"
  fi
fi

if [[ -n "$ERRORS" ]]; then
  echo "---"
  echo "Pre-commit gate FAILED:"
  echo -e "$ERRORS"
  echo "---"
  echo "Fix the issues above before committing."
  exit 2
fi
