#!/usr/bin/env bash
# Shared CodeGraph MCP launcher for Claude / ZCode / Codex / Trae / VS Code.
#
# Resolves the session worktree (never ~/deneb), evicts a stale daemon, then
# wraps `codegraph serve --mcp` with the explore→node proxy so every IDE gets
# the same precision as the runtime agent.
set -euo pipefail

export CODEGRAPH_MCP_TOOLS="${CODEGRAPH_MCP_TOOLS:-explore,node,search,impact,callers,callees}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PRINT_ROOT=0
SERVE_ROOT=""
AGENT="${CODEGRAPH_AGENT:-auto}"
FALLBACK="$(pwd)"
passthrough=()
prev=""
for a in "$@"; do
  if [[ "$a" == "--print-root" ]]; then
    PRINT_ROOT=1
    continue
  fi
  if [[ "$prev" == "-p" || "$prev" == "--path" ]]; then
    SERVE_ROOT="$a"
    prev=""
    continue
  fi
  if [[ "$a" == "-p" || "$a" == "--path" ]]; then
    prev="$a"
    continue
  fi
  if [[ "$prev" == "--agent" ]]; then
    AGENT="$a"
    prev=""
    continue
  fi
  if [[ "$a" == "--agent" ]]; then
    prev="$a"
    continue
  fi
  if [[ "$prev" == "--fallback" ]]; then
    FALLBACK="$a"
    prev=""
    continue
  fi
  if [[ "$a" == "--fallback" ]]; then
    prev="$a"
    continue
  fi
  if [[ -z "$SERVE_ROOT" && -d "$a" ]]; then
    FALLBACK="$a"
    continue
  fi
  passthrough+=("$a")
done

if [[ -z "$SERVE_ROOT" ]]; then
  SERVE_ROOT=$(python3 "$HERE/codegraph_root.py" --agent "$AGENT" --fallback "$FALLBACK")
fi

if [[ "$PRINT_ROOT" -eq 1 ]]; then
  printf '%s\n' "$SERVE_ROOT"
  exit 0
fi

python3 "$HERE/codegraph_daemon.py" evict "$SERVE_ROOT" >/dev/null 2>&1 || true

CG=""
for c in codegraph \
         "${HOME}/.local/bin/codegraph" \
         "${HOME}/.npm-global/bin/codegraph"; do
  if command -v "$c" >/dev/null 2>&1; then
    CG="$c"
    break
  fi
done
if [[ -z "$CG" ]]; then
  echo "codegraph not found (install: npm i -g @colbymchenry/codegraph)" >&2
  exit 127
fi

# Login shell restores node + codegraph PATH for minimal hook/MCP envs.
exec bash -lc 'exec python3 "$0" -- "$1" serve --mcp -p "$2" "${@:3}"' \
  "$HERE/codegraph_mcp_proxy.py" "$CG" "$SERVE_ROOT" "${passthrough[@]}"
