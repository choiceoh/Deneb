#!/bin/bash
# ZCode → Claude hook bridge: adapts ZCode's hook env/payload for Claude scripts.
#
# Claude Code hook scripts (codegraph-nudge.py, codegraph-remind.py,
# claude-rules-gate.py) read:
#   - root:   os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd")
#   - session: payload.get("session_id")
#
# ZCode injects CLAUDE_PROJECT_DIR and CLAUDE_SESSION_ID as env vars, but the
# stdin JSON payload may not carry cwd/session_id keys.  This bridge ensures
# both are present so the Claude scripts work unmodified under ZCode.
#
# Usage:  bash zcode-hook-bridge.sh <script.py>
# stdin:  the ZCode hook payload JSON (passed through, enriched)
set -uo pipefail

TARGET="${1:-}"
[[ -n "$TARGET" && -f "$TARGET" ]] || exit 0

# Read stdin once, enrich, re-emit to the target script.
INPUT=$(cat)

# Ensure CLAUDE_PROJECT_DIR is set (ZCode also sets ZCODE_PROJECT_DIR).
: "${CLAUDE_PROJECT_DIR:=${ZCODE_PROJECT_DIR:-}}"
export CLAUDE_PROJECT_DIR

# Enrich the JSON payload with cwd and session_id if missing, so Claude
# scripts that read payload.get("cwd") / payload.get("session_id") work.
SESSION_ID="${CLAUDE_SESSION_ID:-}"
CWD="${CLAUDE_PROJECT_DIR:-$(pwd)}"

ENRICHED=$(echo "$INPUT" | jq -c \
  --arg cwd "$CWD" \
  --arg sid "$SESSION_ID" \
  '.cwd = (.cwd // $cwd) | .session_id = (.session_id // $sid)' 2>/dev/null \
) || ENRICHED="$INPUT"

# Run the target script with the enriched payload.  Propagate exit code.
echo "$ENRICHED" | python3 "$TARGET"
