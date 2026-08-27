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
# Usage:  bash zcode-hook-bridge.sh <script.py> [mode]
# stdin:  the ZCode hook payload JSON (passed through, enriched)
# mode "claim": translate a Claude permissionDecision=deny JSON into ZCode's
#   one feedback channel — exit 2 with the reason on stderr. Together with the
#   guard's warn-once ledger this is block-once: the denial carries the story
#   and the session's retry passes (the same conversation the codegraph nudge
#   and the rules-gate already have with agents here).
set -uo pipefail

TARGET="${1:-}"
MODE="${2:-passthrough}"
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

# Run the target script with the enriched payload.
if [[ "$MODE" == "claim" ]]; then
  OUT=$(echo "$ENRICHED" | python3 "$TARGET" 2>/dev/null) || exit 0  # fail-open
  if [[ -n "$OUT" ]]; then
    DECISION=$(echo "$OUT" | jq -r '.hookSpecificOutput.permissionDecision // empty' 2>/dev/null || true)
    if [[ "$DECISION" == "deny" ]]; then
      echo "$OUT" | jq -r '.hookSpecificOutput.permissionDecisionReason // "blocked"' >&2 2>/dev/null         || echo "blocked by concurrency guard" >&2
      exit 2
    fi
  fi
  exit 0
fi
# Propagate exit code (passthrough).
echo "$ENRICHED" | python3 "$TARGET"
