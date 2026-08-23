#!/usr/bin/env bash
# Cursor CodeGraph MCP launcher — prefers the session worktree over workspaceFolder.
#
# Cursor often opens the primary checkout; sessionStart then creates
# ~/.cursor/worktrees/Deneb/<session> and writes active-root. Serving CodeGraph
# against workspaceFolder would keep exploring the stale main index while edits
# land in the worktree. This wrapper binds `codegraph serve --mcp -p <active>`.
#
# Resolution order:
#   1) $CURSOR_WORKTREE / $DENEB_AGENT_ROOT (hook env, if present)
#   2) ~/.cursor/worktrees/Deneb/active-root (written by cursor-worktree-init.sh)
#   3) newest ~/.cursor/worktrees/Deneb/*/ that shares this repo's git common dir
#   4) $CURSOR_CODEGRAPH_FALLBACK or cwd / first CLI arg directory
#
# Never bind to ~/deneb (production checkout). `codegraph upgrade` rewrites
# .cursor/mcp.json / .mcp.json to a bare `codegraph serve --mcp --path …` —
# restore these wrappers; do not commit the upgrade output.
set -euo pipefail

WT_BASE="${HOME}/.cursor/worktrees/Deneb"
PROD_CHECKOUT="${HOME}/deneb"
DEV_CHECKOUT="${HOME}/deneb-dev"

resolve_cg() {
  for c in codegraph \
           "${HOME}/.local/bin/codegraph" \
           "${HOME}/.npm-global/bin/codegraph"; do
    if command -v "$c" >/dev/null 2>&1; then
      printf '%s\n' "$c"
      return 0
    fi
  done
  return 1
}

same_repo() {
  local candidate="$1" anchor="$2"
  [[ -d "$candidate" && -d "$anchor" ]] || return 1
  local a b
  a=$(git -C "$candidate" rev-parse --git-common-dir 2>/dev/null) || return 1
  b=$(git -C "$anchor" rev-parse --git-common-dir 2>/dev/null) || return 1
  a=$(cd "$candidate" && cd "$a" && pwd)
  b=$(cd "$anchor" && cd "$b" && pwd)
  [[ "$a" == "$b" ]]
}

is_prod_checkout() {
  local candidate="$1"
  [[ -n "$candidate" && -d "$candidate" && -d "$PROD_CHECKOUT" ]] || return 1
  local a b
  a=$(cd "$candidate" && pwd)
  b=$(cd "$PROD_CHECKOUT" && pwd)
  [[ "$a" == "$b" ]]
}

usable_root() {
  local candidate="$1" anchor="$2"
  [[ -n "$candidate" && -d "$candidate" ]] || return 1
  is_prod_checkout "$candidate" && return 1
  same_repo "$candidate" "$anchor"
}

pick_root() {
  local anchor fallback candidate
  fallback="${CURSOR_CODEGRAPH_FALLBACK:-}"
  if [[ -z "$fallback" ]]; then
    if [[ -n "${1:-}" && -d "${1:-}" ]]; then
      fallback="$1"
    else
      fallback="$(pwd)"
    fi
  fi
  anchor="$fallback"

  for candidate in "${CURSOR_WORKTREE:-}" "${DENEB_AGENT_ROOT:-}"; do
    if usable_root "$candidate" "$anchor"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  # Prefer session-scoped pin over the last-writer global active-root.
  local sid="${CURSOR_SESSION_ID:-}"
  if [[ -n "$sid" ]]; then
    local safe
    safe=$(printf '%s' "$sid" | tr -c 'A-Za-z0-9._-' '_' | cut -c1-80)
    if usable_root "$WT_BASE/$safe" "$anchor"; then
      printf '%s\n' "$WT_BASE/$safe"
      return 0
    fi
    if [[ -n "$safe" && -f "$WT_BASE/active-root.$safe" ]]; then
      candidate=$(head -1 "$WT_BASE/active-root.$safe" 2>/dev/null || true)
      if usable_root "$candidate" "$anchor"; then
        printf '%s\n' "$candidate"
        return 0
      fi
    fi
  fi

  if [[ -f "$WT_BASE/active-root" ]]; then
    candidate=$(head -1 "$WT_BASE/active-root" 2>/dev/null || true)
    if usable_root "$candidate" "$anchor"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  fi

  if [[ -d "$WT_BASE" ]]; then
    # Newest session worktree that belongs to this repo.
    while IFS= read -r candidate; do
      candidate=${candidate%/}
      [[ -d "$candidate" ]] || continue
      if usable_root "$candidate" "$anchor"; then
        printf '%s\n' "$candidate"
        return 0
      fi
    done < <(ls -1dt "$WT_BASE"/*/ 2>/dev/null || true)
  fi

  if is_prod_checkout "$fallback" && [[ -d "$DEV_CHECKOUT" ]]; then
    printf '%s\n' "$DEV_CHECKOUT"
    return 0
  fi
  printf '%s\n' "$fallback"
}

# Optional first arg: fallback directory (Cursor may pass nothing).
# `--print-root` is the test/debug hook — resolve and exit without serving.
PRINT_ROOT=0
if [[ "${1:-}" == "--print-root" ]]; then
  PRINT_ROOT=1
  shift
fi

FALLBACK_ARG=""
if [[ -n "${1:-}" && -d "${1:-}" ]]; then
  FALLBACK_ARG="$1"
  shift
fi

ROOT="$(pick_root ${FALLBACK_ARG:+"$FALLBACK_ARG"})"
if [[ "$PRINT_ROOT" -eq 1 ]]; then
  printf '%s\n' "$ROOT"
  exit 0
fi
export CODEGRAPH_MCP_TOOLS="${CODEGRAPH_MCP_TOOLS:-explore,node,search,impact,callers,callees}"

CG="$(resolve_cg)" || {
  echo "codegraph not found (install: npm i -g @colbymchenry/codegraph)" >&2
  exit 127
}

# Login shell restores node + codegraph PATH for minimal hook/MCP envs.
exec bash -lc 'exec "$0" serve --mcp -p "$1" "${@:2}"' "$CG" "$ROOT" "$@"
