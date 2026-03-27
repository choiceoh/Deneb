#!/usr/bin/env bash
# Cloud environment setup for Claude Code sessions.
#
# Installs system dependencies that are missing in ephemeral sandbox environments.
# Called by .claude/settings.local.json SessionStart hook.
#
# Dependencies installed:
#   - protobuf-compiler (protoc): required by core-rs/core/build.rs (prost-build)
#   - Go 1.25.x: required by gateway-go/go.mod
#   - protoc-gen-go: required by scripts/proto-gen.sh
#   - buf: required by scripts/proto-gen.sh (proto linting/generation)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_VERSION="1.25.8"
REQUIRED_GO_MAJOR_MINOR="1.25"

info() { echo "[setup] $*"; }
warn() { echo "[setup] WARN: $*" >&2; }

# --- protoc ---
if ! command -v protoc &>/dev/null; then
    info "Installing protobuf-compiler..."
    apt-get install -y -qq protobuf-compiler > /dev/null 2>&1
fi

# --- Go ---
CURRENT_GO=""
if command -v go &>/dev/null; then
    CURRENT_GO=$(go version | grep -oP '\d+\.\d+' | head -1)
fi

if [[ "$CURRENT_GO" != "$REQUIRED_GO_MAJOR_MINOR" ]]; then
    info "Installing Go ${GO_VERSION} (current: ${CURRENT_GO:-none})..."
    if curl -sL --max-time 120 "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz 2>/dev/null; then
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tar.gz
        rm -f /tmp/go.tar.gz
        export PATH="/usr/local/go/bin:$PATH"
        info "Go $(go version | grep -oP 'go[\d.]+') installed"
    else
        warn "Go download failed (network restriction?) — skipping"
    fi
else
    info "Go ${CURRENT_GO} already installed"
fi

# --- protoc-gen-go ---
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"
if ! command -v protoc-gen-go &>/dev/null; then
    info "Installing protoc-gen-go..."
    if go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 2>/dev/null; then
        info "protoc-gen-go installed"
    else
        warn "protoc-gen-go install failed — skipping"
    fi
fi

# --- buf ---
if ! command -v buf &>/dev/null; then
    info "Installing buf..."
    if go install github.com/bufbuild/buf/cmd/buf@latest 2>/dev/null; then
        info "buf installed"
    else
        warn "buf install failed — skipping (proto linting unavailable)"
    fi
fi

# --- Build Rust core ---
info "Building Rust core library..."
cd "$REPO_ROOT"
make rust 2>&1 | tail -5

info "Environment setup complete"
