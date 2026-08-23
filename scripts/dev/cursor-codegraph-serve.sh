#!/usr/bin/env bash
# Cursor CodeGraph MCP launcher — binds the session worktree, not workspaceFolder.
#
# Resolution lives in codegraph_root.py (shared with ZCode/Claude/Codex/Trae).
# This wrapper only forces --agent cursor and keeps the historical CLI:
#   cursor-codegraph-serve.sh [--print-root] [fallback-dir]
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PRINT_ROOT=0
if [[ "${1:-}" == "--print-root" ]]; then
  PRINT_ROOT=1
  shift
fi

FALLBACK="${CURSOR_CODEGRAPH_FALLBACK:-}"
if [[ -z "$FALLBACK" && -n "${1:-}" && -d "${1:-}" ]]; then
  FALLBACK="$1"
  shift
fi

ARGS=(--agent cursor)
if [[ -n "$FALLBACK" ]]; then
  ARGS+=(--fallback "$FALLBACK")
fi
if [[ "$PRINT_ROOT" -eq 1 ]]; then
  ARGS+=(--print-root)
fi

export CODEGRAPH_AGENT=cursor
exec bash "$HERE/codegraph-serve.sh" "${ARGS[@]}" "$@"
