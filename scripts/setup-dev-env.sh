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

install_protoc() {
    if command -v protoc &>/dev/null; then return 0; fi
    local version="25.1"
    local arch
    arch=$(uname -m)
    case "$arch" in
        aarch64|arm64) arch="linux-aarch_64" ;;
        x86_64)        arch="linux-x86_64" ;;
        *) return 1 ;;
    esac
    curl -sSL "https://github.com/protocolbuffers/protobuf/releases/download/v${version}/protoc-${version}-${arch}.zip" -o /tmp/protoc.zip \
        && unzip -o /tmp/protoc.zip -d /usr/local bin/protoc 'include/*' >/dev/null 2>&1 \
        && rm -f /tmp/protoc.zip \
        && touch "$SETUP_TMPDIR/installed_protoc"
}

install_buf() {
    if command -v buf &>/dev/null; then return 0; fi
    curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m)" -o /usr/local/bin/buf \
        && chmod +x /usr/local/bin/buf \
        && touch "$SETUP_TMPDIR/installed_buf"
}

install_protoc_gen_go() {
    if command -v protoc-gen-go &>/dev/null; then return 0; fi
    GOFLAGS="-modcacherw" go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 2>/dev/null \
        && touch "$SETUP_TMPDIR/installed_protoc_gen_go"
}

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
    (cd "$REPO_ROOT/gateway-go" && CGO_ENABLED=0 go build -trimpath -tags no_ffi -o "$mcp_bin" ./cmd/mcp-server/) 2>/dev/null \
        && touch "$SETUP_TMPDIR/installed_mcp"
}

setup_mcp_json() {
    local mcp_json="$REPO_ROOT/.mcp.json"
    if [ -f "$mcp_json" ]; then return 0; fi
    cat > "$mcp_json" <<'MCPEOF'
{
  "mcpServers": {
    "deneb": {
      "command": "/home/user/Deneb/bin/deneb-mcp",
      "args": ["--gateway-url", "http://127.0.0.1:18789", "--verbose"]
    }
  }
}
MCPEOF
}

# ---------- Rust core build ----------

build_rust_core() {
    local rust_lib="$REPO_ROOT/core-rs/target/release/libdeneb_core.a"

    if [ -f "$rust_lib" ]; then
        # Source-based staleness: compare lib mtime against newest source file
        local lib_mtime
        lib_mtime=$(stat -c %Y "$rust_lib" 2>/dev/null || echo "0")
        local newest_src
        newest_src=$(find "$REPO_ROOT/core-rs" \( -name '*.rs' -o -name 'Cargo.toml' -o -name 'Cargo.lock' \) -newer "$rust_lib" -print -quit 2>/dev/null)
        if [ -z "$newest_src" ]; then
            # Lib is up-to-date
            return 0
        fi
    fi

    # Build in background so hook completes quickly
    nohup make -C "$REPO_ROOT" rust >/tmp/deneb-rust-build.log 2>&1 &
    touch "$SETUP_TMPDIR/rust_bg_build"
}

# ---------- Main ----------

echo "Deneb dev environment setup"
echo "==========================="
echo ""

# Run independent installs in parallel
install_protoc &
install_buf &
install_protoc_gen_go &
download_go_modules &
wait

# Build MCP server + write .mcp.json (needs go modules ready)
build_mcp_server &
setup_mcp_json
wait

# Build Rust core (may start background build)
build_rust_core

# ---------- Compact summary ----------

installed_count=$(find "$SETUP_TMPDIR" -name 'installed_*' 2>/dev/null | wc -l)
missing=0

# Collect tool versions
rust_ver=$(rustc --version 2>/dev/null | grep -oP '\d+\.\d+\.\d+' || echo "missing") ; [ "$rust_ver" = "missing" ] && missing=$((missing + 1))
go_ver=$(go version 2>/dev/null | grep -oP 'go\d+\.\d+\.\d*' || echo "missing") ; [ "$go_ver" = "missing" ] && missing=$((missing + 1))
protoc_ver=$(protoc --version 2>/dev/null | grep -oP '\d+\.\d+' || echo "missing") ; [ "$protoc_ver" = "missing" ] && missing=$((missing + 1))
buf_ver=$(buf --version 2>/dev/null | head -1 || echo "missing") ; [ "$buf_ver" = "missing" ] && missing=$((missing + 1))
pgg_status="ok" ; command -v protoc-gen-go &>/dev/null || { pgg_status="missing"; missing=$((missing + 1)); }

# Go modules status
go_mod_status="cached"
if ! (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then
    go_mod_status="missing"
fi

# Rust lib status
rust_lib_status="fresh"
if [ -f "$SETUP_TMPDIR/rust_bg_build" ]; then
    rust_lib_status="building (background, ~60s)"
elif [ ! -f "$REPO_ROOT/core-rs/target/release/libdeneb_core.a" ]; then
    rust_lib_status="missing"
fi

echo "  [env] rust=$rust_ver $go_ver protoc=$protoc_ver buf=$buf_ver protoc-gen-go=$pgg_status"
# MCP server status
mcp_status="ready"
if [ ! -f "$REPO_ROOT/bin/deneb-mcp" ]; then
    mcp_status="missing"
fi
mcp_json_status="ready"
if [ ! -f "$REPO_ROOT/.mcp.json" ]; then
    mcp_json_status="missing"
fi

echo "  [env] go-modules=$go_mod_status libdeneb_core.a=$rust_lib_status"
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
echo "Build order: make rust -> make go -> make test"
echo "Fast iteration: make rust-debug -> make go-dev"

exit 0
