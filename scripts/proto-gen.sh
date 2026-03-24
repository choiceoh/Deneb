#!/usr/bin/env bash
# Protobuf code generation pipeline.
#
# Generates Go, Rust, and TypeScript types from proto/ definitions.
#
# Prerequisites:
#   - buf (https://buf.build/docs/installation)
#   - protoc (apt install protobuf-compiler / brew install protobuf)
#   - Rust toolchain (for prost-build via cargo build)
#
# Usage:
#   ./scripts/proto-gen.sh          # generate all
#   ./scripts/proto-gen.sh --go     # Go only
#   ./scripts/proto-gen.sh --rust   # Rust only
#   ./scripts/proto-gen.sh --ts     # TypeScript only
#   ./scripts/proto-gen.sh --check  # generate + verify no uncommitted diffs

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Ensure Go bin is in PATH (protoc-gen-go).
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"
PROTO_DIR="$REPO_ROOT/proto"
GO_OUT="$REPO_ROOT/gateway-go/pkg/protocol/gen"
TS_OUT="$REPO_ROOT/src/protocol/generated"

# Colors for output.
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[proto-gen]${NC} $*"; }
warn() { echo -e "${YELLOW}[proto-gen]${NC} $*"; }
fail() { echo -e "${RED}[proto-gen]${NC} $*" >&2; exit 1; }

# --- Dependency checks ---

check_buf() {
  if ! command -v buf &>/dev/null; then
    fail "buf not found. Install: https://buf.build/docs/installation"
  fi
}

check_protoc() {
  if ! command -v protoc &>/dev/null; then
    fail "protoc not found. Install: apt install protobuf-compiler / brew install protobuf"
  fi
}

check_cargo() {
  if ! command -v cargo &>/dev/null; then
    fail "cargo not found. Install: https://rustup.rs"
  fi
}

# --- Generators ---

gen_go() {
  info "Generating Go structs → gateway-go/pkg/protocol/gen/"
  check_buf
  mkdir -p "$GO_OUT"
  cd "$PROTO_DIR"
  buf generate --template buf.gen.yaml --path gateway.proto --path channel.proto --path session.proto 2>/dev/null || {
    # Fallback: try protoc directly if buf remote plugins aren't available.
    warn "buf remote plugins unavailable, falling back to protoc"
    check_protoc
    if ! command -v protoc-gen-go &>/dev/null; then
      fail "protoc-gen-go not found. Install: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    fi
    protoc \
      --go_out="$GO_OUT" \
      --go_opt=paths=source_relative \
      -I "$PROTO_DIR" \
      "$PROTO_DIR"/*.proto
  }
  info "Go generation complete"
}

gen_rust() {
  info "Generating Rust structs → core-rs/src/protocol/ (via prost-build)"
  check_cargo
  cd "$REPO_ROOT/core-rs"
  cargo build 2>&1 | tail -5
  info "Rust generation complete"
}

gen_ts() {
  info "Generating TypeScript types → src/protocol/generated/"
  check_buf
  mkdir -p "$TS_OUT"
  cd "$PROTO_DIR"
  buf generate --template buf.gen.yaml --path gateway.proto --path channel.proto --path session.proto 2>/dev/null || {
    warn "buf remote plugins unavailable for TS generation, skipping (use buf login or install ts-proto locally)"
    return 0
  }
  info "TypeScript generation complete"
}

gen_all() {
  gen_go
  gen_rust
  gen_ts
}

check_diffs() {
  info "Checking for uncommitted generated diffs..."
  cd "$REPO_ROOT"
  local paths=(
    "gateway-go/pkg/protocol/gen"
    "src/protocol/generated"
  )
  if ! git diff --exit-code -- "${paths[@]}" >/dev/null 2>&1; then
    fail "Generated protobuf code is out of date. Run: ./scripts/proto-gen.sh"
  fi
  info "All generated code is up to date"
}

# --- Main ---

case "${1:-all}" in
  --go)    gen_go ;;
  --rust)  gen_rust ;;
  --ts)    gen_ts ;;
  --check) gen_all; check_diffs ;;
  all|"")  gen_all ;;
  *)       fail "Unknown option: $1. Use --go, --rust, --ts, --check, or no args." ;;
esac
