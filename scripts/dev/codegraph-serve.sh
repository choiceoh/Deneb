#!/usr/bin/env bash
# Robust launcher for CodeGraph's MCP server (wired from .mcp.json / .zcode).
#
# Claude Code may spawn the MCP server (and hooks) with a MINIMAL PATH — the
# login profile isn't sourced, so neither `codegraph` (~/.local/bin) NOR the
# `node` it runs on (~/.local/node/bin) is on PATH, and a bare `codegraph`
# silently fails. Re-exec through a LOGIN shell so the profile PATH (node +
# codegraph) is restored, with an explicit-spot fallback. Portable — no
# machine-specific paths baked in.
#
# CODEGRAPH_MCP_TOOLS: default MCP surface is explore-only; re-enable node/
# search/impact/callers so agents can pin exact symbols instead of fuzzy bags.
set -eu

export CODEGRAPH_MCP_TOOLS="${CODEGRAPH_MCP_TOOLS:-explore,node,search,impact,callers,callees}"

exec bash -lc '
  for c in codegraph "$HOME/.local/bin/codegraph" "$HOME/.npm-global/bin/codegraph"; do
    if command -v "$c" >/dev/null 2>&1; then exec "$c" serve --mcp "$@"; fi
  done
  echo "codegraph not found (install: npm i -g @colbymchenry/codegraph)" >&2
  exit 127
' _ "$@"
