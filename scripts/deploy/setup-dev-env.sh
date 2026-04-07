#!/usr/bin/env bash
# setup-dev-env.sh — Auto-install missing development prerequisites and build artifacts.
# Designed for Claude Code web sessions where tools may not persist between sessions.
# Non-blocking: always exits 0 so sessions can start.

set -u

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SETUP_TMPDIR=$(mktemp -d)
trap 'rm -rf "$SETUP_TMPDIR"' EXIT

# ---------- Network fix for Claude Code web containers ----------
# In Claude Code web containers, NO_PROXY includes *.googleapis.com and *.google.com
# which prevents Go from using the egress proxy for module downloads (Go uses UDP DNS
# which is blocked, while curl uses the proxy's DNS). Remove these entries so Go
# traffic goes through the proxy correctly.
if [[ "${CLAUDE_CODE_PROXY_RESOLVES_HOSTS:-}" == "true" ]]; then
    cleaned=$(echo "${NO_PROXY:-}" | sed 's/\*\.googleapis\.com//g; s/\*\.google\.com//g' | sed 's/,,*/,/g; s/^,//; s/,$//')
    export NO_PROXY="$cleaned"
    export no_proxy="$cleaned"
    export GLOBAL_AGENT_NO_PROXY="$cleaned"
fi

# Ensure GOPATH/bin is on PATH so `go install` binaries are discoverable
gobin="$(go env GOPATH 2>/dev/null)/bin"
if [[ -n "$gobin" && ":$PATH:" != *":$gobin:"* ]]; then
    export PATH="$gobin:$PATH"
fi

# ---------- Tool installers (each writes a marker file on success) ----------

download_go_modules() {
    # Skip download if cache is already valid
    if (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then return 0; fi
    (cd "$REPO_ROOT/gateway-go" && GOFLAGS="-modcacherw" go mod download) 2>/dev/null \
        && touch "$SETUP_TMPDIR/installed_go_modules"
}

# ---------- MCP server build ----------

build_mcp_server() {
    local mcp_bin="$REPO_ROOT/bin/deneb-mcp"
    if [ -f "$mcp_bin" ]; then return 0; fi
    (cd "$REPO_ROOT/gateway-go" && CGO_ENABLED=0 go build -trimpath -o "$mcp_bin" ./cmd/mcp-server/) 2>/dev/null \
        && touch "$SETUP_TMPDIR/installed_mcp"
}

setup_mcp_json() {
    local mcp_json="$REPO_ROOT/.mcp.json"
    if [ -f "$mcp_json" ]; then return 0; fi
    local gh_token
    gh_token=$(gh auth token 2>/dev/null || echo "")
    cat > "$mcp_json" <<MCPEOF
{
  "mcpServers": {
    "deneb": {
      "command": "$REPO_ROOT/bin/deneb-mcp",
      "args": ["--gateway-url", "http://127.0.0.1:18789", "--verbose"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "$gh_token"
      }
    }
  }
}
MCPEOF
}

# ---------- Main ----------

echo "Deneb dev environment setup"
echo "==========================="
echo "  [context] Claude agent running on Deneb gateway server (DGX Spark)"
echo ""

# Run independent installs in parallel
download_go_modules &
wait

# Build MCP server + write .mcp.json (needs go modules ready)
build_mcp_server &
setup_mcp_json
wait

# ---------- Compact summary ----------

installed_count=$(find "$SETUP_TMPDIR" -name 'installed_*' 2>/dev/null | wc -l)
missing=0

# Collect tool versions
go_ver=$(go version 2>/dev/null | grep -oP 'go\d+\.\d+\.\d*' || echo "missing") ; [ "$go_ver" = "missing" ] && missing=$((missing + 1))

# Go modules status
go_mod_status="cached"
if ! (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then
    go_mod_status="missing"
fi

echo "  [env] $go_ver"
# MCP server status
mcp_status="ready"
if [ ! -f "$REPO_ROOT/bin/deneb-mcp" ]; then
    mcp_status="missing"
fi
mcp_json_status="ready"
if [ ! -f "$REPO_ROOT/.mcp.json" ]; then
    mcp_json_status="missing"
fi

echo "  [env] go-modules=$go_mod_status"
echo "  [env] deneb-mcp=$mcp_status .mcp.json=$mcp_json_status"

# Go version compatibility warning
go_minor=$(echo "$go_ver" | grep -oP '(?<=go)\d+\.\d+' || echo "0.0")
if [[ "$go_minor" < "1.24" && "$go_ver" != "missing" ]]; then
    echo "  [warn] Go $go_ver < 1.24 required"
fi

if [ "$missing" -gt 0 ]; then
    echo "  [env] ready ($installed_count installed, $missing missing)"
elif [ "$installed_count" -gt 0 ]; then
    echo "  [env] ready ($installed_count installed)"
else
    echo "  [env] ready"
fi

echo ""
echo "Build: make go -> make test"
echo "Fast iteration: make go-dev"

exit 0
