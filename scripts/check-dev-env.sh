#!/usr/bin/env bash
# check-dev-env.sh — Validate development prerequisites for AI agent sessions.
# Non-blocking: prints warnings but always exits 0 so sessions can start.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MISSING=0

check_tool() {
    local name="$1"
    local cmd="$2"
    local hint="$3"

    if command -v "$cmd" &>/dev/null; then
        local ver
        ver=$("$cmd" --version 2>/dev/null | head -1 || "$cmd" version 2>/dev/null | head -1 || echo "(installed)")
        echo "  [ok] $name: $ver"
    else
        echo "  [missing] $name — $hint"
        MISSING=$((MISSING + 1))
    fi
}

echo "Deneb dev environment check"
echo "==========================="
echo ""
echo "Tools:"
check_tool "Rust compiler" "rustc" "Install via https://rustup.rs"
check_tool "Cargo" "cargo" "Install via https://rustup.rs"
check_tool "Go" "go" "Install Go 1.24+ from https://go.dev/dl"
check_tool "protoc" "protoc" "apt-get install protobuf-compiler (or https://github.com/protocolbuffers/protobuf/releases)"
check_tool "buf" "buf" "Install from https://buf.build/docs/installation"
check_tool "protoc-gen-go" "protoc-gen-go" "go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
check_tool "ripgrep" "rg" "cargo install ripgrep (or apt-get install ripgrep)"

echo ""
echo "Go modules:"
if (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then
    echo "  [ok] Go module cache verified"
else
    echo "  [missing] Go modules not cached — run 'cd gateway-go && go mod download'"
    MISSING=$((MISSING + 1))
fi

echo ""
echo "Build artifacts:"

RUST_LIB="$REPO_ROOT/core-rs/target/release/libdeneb_core.a"
if [ -f "$RUST_LIB" ]; then
    local_mtime=$(stat -c %Y "$RUST_LIB" 2>/dev/null || stat -f %m "$RUST_LIB" 2>/dev/null || echo "0")
    local_age=$(( $(date +%s) - local_mtime ))
    if [ "$local_age" -lt 86400 ]; then
        echo "  [ok] libdeneb_core.a (< 1 day old)"
    else
        echo "  [stale] libdeneb_core.a (> 1 day old) — run 'make rust' to rebuild"
    fi
else
    echo "  [missing] libdeneb_core.a — run 'make rust' to build Rust core"
fi

echo ""

if [ "$MISSING" -gt 0 ]; then
    echo "Warning: $MISSING tool(s) missing. Some build targets may fail."
    echo "See CLAUDE.md 'Agent Quick-Start' section for setup instructions."
else
    echo "All tools available. Ready to build."
fi

echo ""
echo "Build order: make rust -> make go -> make test"
echo "Fast iteration: make rust-debug -> make go-dev"

# Always exit 0 so the session can start regardless.
exit 0
