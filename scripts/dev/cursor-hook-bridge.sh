#!/usr/bin/env bash
# Adapt Cursor preToolUse payloads for Claude-oriented hook scripts, and map
# Claude deny (exit 2 + stderr) / Claude remind JSON into Cursor hook JSON.
#
# Usage: bash cursor-hook-bridge.sh <script.py> <mode>
#   mode: nudge | remind | gate | passthrough
set -uo pipefail

TARGET="${1:-}"
MODE="${2:-passthrough}"
[[ -n "$TARGET" && -f "$TARGET" ]] || exit 0

INPUT=$(cat)

# Prefer session-pinned worktree as project root for CodeGraph queries.
ROOT="${CURSOR_WORKTREE:-${DENEB_AGENT_ROOT:-${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}}}"
ROOT=$(cd "$ROOT" 2>/dev/null && pwd) || ROOT="$(pwd)"
export CLAUDE_PROJECT_DIR="$ROOT"
export CURSOR_PROJECT_DIR="${CURSOR_PROJECT_DIR:-$ROOT}"

SESSION_ID="${CURSOR_SESSION_ID:-}"
CWD=$(echo "$INPUT" | jq -r '.cwd // empty' 2>/dev/null || true)
[[ -n "$CWD" ]] || CWD="$ROOT"

# Normalize tool names Cursor → Claude scripts expect.
ENRICHED=$(echo "$INPUT" | jq -c \
  --arg cwd "$CWD" \
  --arg sid "$SESSION_ID" \
  '
    .cwd = (.cwd // $cwd)
    | .session_id = (.session_id // $sid)
    | if .tool_name == "Shell" then .tool_name = "Bash" else . end
    # Cursor file tools → the names Claude-oriented scripts key on. Without
    # this, StrReplace edits walk straight past any tool-list check (the
    # concurrency guard included) while looking wired.
    | if .tool_name == "StrReplace" or .tool_name == "EditNotebook" then .tool_name = "Edit" else . end
    | if .tool_name == "Delete" then .tool_name = "Write" else . end
    | if (.tool_input.path != null) and (.tool_input.file_path == null) then
        .tool_input.file_path = .tool_input.path
      else . end
  ' 2>/dev/null) || ENRICHED="$INPUT"

OUT=$(mktemp)
ERR=$(mktemp)
trap 'rm -f "$OUT" "$ERR"' EXIT

set +e
echo "$ENRICHED" | python3 "$TARGET" >"$OUT" 2>"$ERR"
RC=$?
set -e

case "$MODE" in
  claim)
    # Concurrency-guard adapter: the guard's block-once deny maps to Cursor's
    # native deny, message intact — the warn-once ledger makes the retry pass.
    # Registering the claim is a side effect of running the guard at all, which
    # is the half that matters cross-harness: Claude sessions see Cursor's
    # edits and vice versa.
    if [[ "$RC" -eq 2 ]]; then
      MSG=$(cat "$ERR"); [[ -n "$MSG" ]] || MSG="blocked by hook"
      MSG_JSON=$(printf '%s' "$MSG" | jq -Rs .)
      printf '{"permission":"deny","agent_message":%s,"user_message":%s}
' "$MSG_JSON" "$MSG_JSON"
      exit 0
    fi
    if [[ -s "$OUT" ]]; then
      DECISION=$(jq -r '.hookSpecificOutput.permissionDecision // empty' "$OUT" 2>/dev/null || true)
      REASON=$(jq -r '.hookSpecificOutput.permissionDecisionReason // empty' "$OUT" 2>/dev/null || true)
      if [[ "$DECISION" == "deny" ]]; then
        MSG_JSON=$(printf '%s' "$REASON" | jq -Rs .)
        printf '{"permission":"deny","agent_message":%s,"user_message":%s}
' "$MSG_JSON" "$MSG_JSON"
        exit 0
      fi
    fi
    exit 0
    ;;
  nudge|gate)
    if [[ "$RC" -eq 2 ]]; then
      MSG=$(cat "$ERR")
      [[ -n "$MSG" ]] || MSG="blocked by hook"
      MSG_JSON=$(printf '%s' "$MSG" | jq -Rs .)
      printf '{"permission":"deny","agent_message":%s,"user_message":%s}\n' "$MSG_JSON" "$MSG_JSON"
      exit 0
    fi
    if [[ -s "$OUT" ]] && jq -e . >/dev/null 2>&1 <"$OUT"; then
      cat "$OUT"
    fi
    exit 0
    ;;
  remind)
    if [[ -s "$OUT" ]]; then
      CTX=$(jq -r '.hookSpecificOutput.additionalContext // .additional_context // empty' "$OUT" 2>/dev/null || true)
      if [[ -n "$CTX" ]]; then
        CTX_JSON=$(printf '%s' "$CTX" | jq -Rs .)
        printf '{"additional_context":%s}\n' "$CTX_JSON"
      fi
    fi
    exit 0
    ;;
  *)
    [[ -s "$OUT" ]] && cat "$OUT"
    exit 0
    ;;
esac
