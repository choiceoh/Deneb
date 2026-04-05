#!/bin/bash
# PostToolUse hook: notify Deneb main agent about key events via bridge.
# Triggers on: git commit, git push, gh pr create
set -euo pipefail

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
TOOL_INPUT=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
TOOL_OUTPUT=$(echo "$INPUT" | jq -r '.tool_output // empty' | head -c 500)

[[ "$TOOL_NAME" == "Bash" ]] || exit 0

# Determine event type
EVENT=""
MSG=""

# Worktree name for identification
WORKTREE=$(basename "$(git rev-parse --show-toplevel 2>/dev/null)" 2>/dev/null || echo "unknown")

case "$TOOL_INPUT" in
  *"git commit"*|*"scripts/committer"*)
    # Extract commit message from output
    COMMIT_MSG=$(echo "$TOOL_OUTPUT" | grep -oP '(?<=\] ).*' | head -1)
    [[ -n "$COMMIT_MSG" ]] || exit 0
    EVENT="commit"
    MSG="[$WORKTREE] 커밋: $COMMIT_MSG"
    ;;
  *"gh pr create"*)
    # Extract PR URL from output
    PR_URL=$(echo "$TOOL_OUTPUT" | grep -oP 'https://github\.com/[^\s]+' | head -1)
    [[ -n "$PR_URL" ]] || exit 0
    EVENT="pr"
    MSG="[$WORKTREE] PR 생성: $PR_URL"
    ;;
  *)
    exit 0
    ;;
esac

[[ -n "$MSG" ]] || exit 0

# Send bridge notification (fire-and-forget, don't block the session)
BODY=$(python3 -c "
import json,sys
print(json.dumps({
    'type':'req','id':'hook-$EVENT','method':'bridge.send',
    'params':{'message':sys.argv[1],'source':'claude-code:$WORKTREE'}
}))" "$MSG")

curl -s -X POST http://127.0.0.1:18789/api/v1/rpc \
  -H "Content-Type: application/json" \
  -d "$BODY" >/dev/null 2>&1 &

exit 0
