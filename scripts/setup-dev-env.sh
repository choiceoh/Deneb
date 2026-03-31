#!/usr/bin/env bash
# setup-dev-env.sh — Auto-install missing development prerequisites and build artifacts.
# Designed for Claude Code web sessions where tools may not persist between sessions.
# Non-blocking: always exits 0 so sessions can start.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALLED=0

# ---------- Network fix for Claude Code web containers ----------
# In Claude Code web containers, NO_PROXY includes *.googleapis.com and *.google.com
# which prevents Go from using the egress proxy for module downloads (Go uses UDP DNS
# which is blocked, while curl uses the proxy's DNS). Remove these entries so Go
# traffic goes through the proxy correctly.
fix_no_proxy() {
    if [[ "${CLAUDE_CODE_PROXY_RESOLVES_HOSTS:-}" == "true" ]]; then
        local cleaned
        cleaned=$(echo "${NO_PROXY:-}" | sed 's/\*\.googleapis\.com//g; s/\*\.google\.com//g' | sed 's/,,*/,/g; s/^,//; s/,$//')
        export NO_PROXY="$cleaned"
        export no_proxy="$cleaned"
        export GLOBAL_AGENT_NO_PROXY="$cleaned"
    fi
}

# ---------- Tool installers ----------

install_protoc() {
    if command -v protoc &>/dev/null; then return 0; fi
    echo "  [setup] Installing protoc..."
    local version="25.1"
    local arch
    arch=$(uname -m)
    case "$arch" in
        aarch64|arm64) arch="linux-aarch_64" ;;
        x86_64)        arch="linux-x86_64" ;;
        *) echo "  [fail] unsupported arch: $arch"; return 1 ;;
    esac
    curl -sSL "https://github.com/protocolbuffers/protobuf/releases/download/v${version}/protoc-${version}-${arch}.zip" -o /tmp/protoc.zip
    unzip -o /tmp/protoc.zip -d /usr/local bin/protoc 'include/*' >/dev/null 2>&1
    rm -f /tmp/protoc.zip
    if command -v protoc &>/dev/null; then
        echo "  [ok] protoc installed: $(protoc --version)"
        INSTALLED=$((INSTALLED + 1))
    else
        echo "  [fail] protoc installation failed"
    fi
}

install_buf() {
    if command -v buf &>/dev/null; then return 0; fi
    echo "  [setup] Installing buf..."
    curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m)" -o /usr/local/bin/buf
    chmod +x /usr/local/bin/buf
    if command -v buf &>/dev/null; then
        echo "  [ok] buf installed: $(buf --version)"
        INSTALLED=$((INSTALLED + 1))
    else
        echo "  [fail] buf installation failed"
    fi
}

install_protoc_gen_go() {
    if command -v protoc-gen-go &>/dev/null; then return 0; fi
    echo "  [setup] Installing protoc-gen-go..."
    fix_no_proxy
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 2>/dev/null
    if command -v protoc-gen-go &>/dev/null; then
        echo "  [ok] protoc-gen-go installed"
        INSTALLED=$((INSTALLED + 1))
    else
        echo "  [fail] protoc-gen-go installation failed"
    fi
}

build_rust_core() {
    local rust_lib="$REPO_ROOT/core-rs/target/release/libdeneb_core.a"
    if [ -f "$rust_lib" ]; then
        local age=$(( $(date +%s) - $(stat -c %Y "$rust_lib" 2>/dev/null || echo "0") ))
        if [ "$age" -lt 86400 ]; then return 0; fi
        echo "  [setup] Rebuilding stale libdeneb_core.a..."
    else
        echo "  [setup] Building libdeneb_core.a (this takes ~60s)..."
    fi
    make -C "$REPO_ROOT" rust 2>&1 | tail -1
    if [ -f "$rust_lib" ]; then
        echo "  [ok] libdeneb_core.a built"
        INSTALLED=$((INSTALLED + 1))
    else
        echo "  [fail] libdeneb_core.a build failed"
    fi
}

# ---------- Main ----------

echo "Deneb dev environment setup"
echo "==========================="
echo ""

# Fix proxy before any network operations
fix_no_proxy

# Install missing tools
install_protoc
install_buf
install_protoc_gen_go

# Build Rust core if missing
build_rust_core

echo ""
if [ "$INSTALLED" -gt 0 ]; then
    echo "Setup: installed/built $INSTALLED component(s)."
else
    echo "All tools and artifacts present. No setup needed."
fi

# Now run the diagnostic check
echo ""
exec "$REPO_ROOT/scripts/check-dev-env.sh"
