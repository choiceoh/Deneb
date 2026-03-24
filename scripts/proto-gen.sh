#!/usr/bin/env bash
# Protobuf code generation pipeline.
#
# Generates Go, Rust, and TypeScript types from proto/ definitions.
#
# Prerequisites:
#   - buf (https://buf.build/docs/installation)
#   - protoc (apt install protobuf-compiler / brew install protobuf)
#   - protoc-gen-go (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
#   - ts-proto (npm install -g ts-proto)
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
PROTO_DIR="$REPO_ROOT/proto"
GO_OUT="$REPO_ROOT/gateway-go/pkg/protocol/gen"
TS_OUT="$REPO_ROOT/src/protocol/generated"
LOCKFILE="$REPO_ROOT/.proto-gen.lock"

# Ensure Go bin is in PATH (protoc-gen-go).
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"

# Colors for output.
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[proto-gen]${NC} $*"; }
warn() { echo -e "${YELLOW}[proto-gen]${NC} $*"; }
fail() { echo -e "${RED}[proto-gen]${NC} $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" &>/dev/null || fail "$1 not found. $2"
}

# --- Concurrency lock ---

acquire_lock() {
  if [ -f "$LOCKFILE" ]; then
    local pid
    pid=$(cat "$LOCKFILE" 2>/dev/null)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      fail "Another proto-gen.sh is running (PID $pid). If stale, remove $LOCKFILE"
    fi
    warn "Removing stale lockfile (PID $pid no longer running)"
  fi
  echo $$ > "$LOCKFILE"
}

release_lock() {
  rm -f "$LOCKFILE"
}

# --- Signal handling ---
# On interrupt, clean up lockfile and restore any partially cleaned output
# from git so the working tree isn't left in a broken state.

cleanup_on_signal() {
  warn "Interrupted — restoring generated files from git..."
  git -C "$REPO_ROOT" checkout -- \
    "gateway-go/pkg/protocol/gen" \
    "src/protocol/generated" 2>/dev/null || true
  release_lock
  exit 130
}

trap cleanup_on_signal INT TERM

# --- Validation helpers ---

verify_proto_sources() {
  [ -d "$PROTO_DIR" ] || fail "Proto directory not found: $PROTO_DIR"
  local count
  count=$(find "$PROTO_DIR" -maxdepth 1 -name "*.proto" -type f | wc -l)
  [ "$count" -gt 0 ] || fail "No .proto files found in $PROTO_DIR"
}

verify_output() {
  local dir="$1" pattern="$2" label="$3"
  local count
  count=$(find "$dir" -type f -name "$pattern" 2>/dev/null | wc -l)
  if [ "$count" -eq 0 ]; then
    fail "$label generation produced no output files in $dir (expected $pattern)"
  fi
  info "$label: generated $count file(s)"
}

clean_generated() {
  local dir="$1"
  if [ -d "$dir" ]; then
    find "$dir" -type f \( -name "*.pb.go" -o -name "*.ts" \) -delete
    find "$dir" -mindepth 1 -type d -empty -delete 2>/dev/null || true
  fi
}

# Run buf lint as a pre-check before generation.
lint_protos() {
  require_cmd buf "Install: https://buf.build/docs/installation"
  info "Linting proto files..."
  (cd "$PROTO_DIR" && buf lint) || fail "buf lint failed — fix proto errors before generating"
}

# --- Generators ---
# Each runs in a subshell to avoid cd side effects.
# Pattern: verify prereqs → clean stale → generate → verify output.

gen_go() {
  info "Generating Go → gateway-go/pkg/protocol/gen/"
  require_cmd buf "Install: https://buf.build/docs/installation"
  require_cmd protoc-gen-go "Install: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
  clean_generated "$GO_OUT"
  (
    mkdir -p "$GO_OUT"
    cd "$PROTO_DIR"
    buf generate --template buf.gen.go.yaml
  )
  verify_output "$GO_OUT" "*.pb.go" "Go"
}

gen_rust() {
  info "Generating Rust via prost-build (output in cargo OUT_DIR)"
  require_cmd cargo "Install: https://rustup.rs"
  require_cmd protoc "Install: apt install protobuf-compiler / brew install protobuf"
  local output
  if ! output=$(cd "$REPO_ROOT/core-rs" && cargo check 2>&1); then
    echo "$output" >&2
    fail "Rust protobuf generation failed (see cargo output above)"
  fi
  info "Rust generation complete"
}

gen_ts() {
  info "Generating TypeScript → src/protocol/generated/"
  require_cmd buf "Install: https://buf.build/docs/installation"
  require_cmd protoc-gen-ts_proto "Install: npm install -g ts-proto"
  clean_generated "$TS_OUT"
  (
    mkdir -p "$TS_OUT"
    cd "$PROTO_DIR"
    buf generate --template buf.gen.ts.yaml
  )
  verify_output "$TS_OUT" "*.ts" "TypeScript"
}

gen_all() {
  verify_proto_sources
  lint_protos

  local failures=()

  gen_go    || failures+=("Go")
  gen_rust  || failures+=("Rust")
  gen_ts    || failures+=("TypeScript")

  if [ ${#failures[@]} -gt 0 ]; then
    fail "Generation failed for: ${failures[*]}"
  fi
}

check_diffs() {
  info "Checking for uncommitted generated diffs..."
  cd "$REPO_ROOT"
  local paths=(
    "gateway-go/pkg/protocol/gen"
    "src/protocol/generated"
  )
  local has_diff=0

  # Check unstaged changes.
  if ! git diff --exit-code -- "${paths[@]}" >/dev/null 2>&1; then
    has_diff=1
  fi

  # Check staged changes.
  if ! git diff --cached --exit-code -- "${paths[@]}" >/dev/null 2>&1; then
    has_diff=1
  fi

  # Check untracked new files.
  if [ -n "$(git ls-files --others --exclude-standard -- "${paths[@]}" 2>/dev/null)" ]; then
    has_diff=1
  fi

  if [ "$has_diff" -eq 1 ]; then
    echo "" >&2
    warn "Changed files:"
    git diff --stat -- "${paths[@]}" >&2 || true
    git diff --cached --stat -- "${paths[@]}" >&2 || true
    local untracked
    untracked=$(git ls-files --others --exclude-standard -- "${paths[@]}" 2>/dev/null)
    if [ -n "$untracked" ]; then
      warn "Untracked files:"
      echo "$untracked" >&2
    fi
    echo "" >&2
    fail "Generated protobuf code is out of date. Run: ./scripts/proto-gen.sh"
  fi
  info "All generated code is up to date"
}

# --- Main ---

acquire_lock
trap 'cleanup_on_signal' INT TERM
trap 'release_lock' EXIT

case "${1:-all}" in
  --go)    verify_proto_sources; lint_protos; gen_go ;;
  --rust)  verify_proto_sources; gen_rust ;;
  --ts)    verify_proto_sources; lint_protos; gen_ts ;;
  --check) gen_all; check_diffs ;;
  all|"")  gen_all ;;
  *)       fail "Unknown option: $1. Use --go, --rust, --ts, --check, or no args." ;;
esac
