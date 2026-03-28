#!/usr/bin/env bash
# Protobuf code generation pipeline.
#
# Generates Go and Rust types from proto/ definitions.
#
# Prerequisites:
#   - buf (https://buf.build/docs/installation)
#   - protoc (apt install protobuf-compiler / brew install protobuf)
#   - protoc-gen-go (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
#   - Rust toolchain (for prost-build via cargo build)
#
# Usage:
#   ./scripts/proto-gen.sh          # generate all (parallel)
#   ./scripts/proto-gen.sh --go     # Go only
#   ./scripts/proto-gen.sh --rust   # Rust only
#   ./scripts/proto-gen.sh --check  # generate + verify no uncommitted diffs
#   ./scripts/proto-gen.sh --lint   # lint proto files only
#   ./scripts/proto-gen.sh --watch  # watch proto files and regenerate on change

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$REPO_ROOT/proto"
GO_OUT="$REPO_ROOT/gateway-go/pkg/protocol/gen"
LOCKFILE="$REPO_ROOT/.proto-gen.lock"
HASH_FILE="$REPO_ROOT/.proto-gen.hash"

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

release_lock() { rm -f "$LOCKFILE"; }

cleanup_on_signal() {
  warn "Interrupted — restoring generated files from git..."
  git -C "$REPO_ROOT" checkout -- \
    "gateway-go/pkg/protocol/gen" 2>/dev/null || true
  release_lock
  exit 130
}

# --- Prerequisite checks (run once) ---

check_prereqs() {
  local need_buf="${1:-}" need_rust="${2:-}"

  [ -d "$PROTO_DIR" ] || fail "Proto directory not found: $PROTO_DIR"
  local count
  count=$(find "$PROTO_DIR" -maxdepth 1 -name "*.proto" -type f | wc -l)
  [ "$count" -gt 0 ] || fail "No .proto files found in $PROTO_DIR"

  if [ "$need_buf" = "buf" ]; then
    command -v buf &>/dev/null || fail "buf not found. Install: https://buf.build/docs/installation"
  fi
  if [ "$need_rust" = "rust" ]; then
    command -v cargo &>/dev/null || fail "cargo not found. Install: https://rustup.rs"
    command -v protoc &>/dev/null || fail "protoc not found. Install: apt install protobuf-compiler / brew install protobuf"
  fi
}

# --- Proto hash for skip optimization ---

compute_proto_hash() {
  # Hash proto files + gen configs to detect changes.
  cat "$PROTO_DIR"/*.proto \
    "$PROTO_DIR"/buf.gen.go.yaml \
    "$REPO_ROOT/core-rs/core/build.rs" \
    2>/dev/null | sha256sum | cut -d' ' -f1
}

is_up_to_date() {
  [ -f "$HASH_FILE" ] && [ "$(cat "$HASH_FILE" 2>/dev/null)" = "$(compute_proto_hash)" ]
}

save_hash() {
  compute_proto_hash > "$HASH_FILE"
}

# --- Helpers ---

verify_output() {
  local dir="$1" pattern="$2" label="$3"
  local count
  count=$(find "$dir" -type f -name "$pattern" 2>/dev/null | wc -l)
  if [ "$count" -eq 0 ]; then
    fail "$label generation produced no output files in $dir (expected $pattern)"
  fi
  info "$label: generated $count file(s)"
}

# Verify that every .proto file in proto/ has a corresponding generated Go output.
verify_completeness() {
  local proto_count go_count missing=()
  local go_parent_dir
  go_parent_dir="$(dirname "$GO_OUT")"
  proto_count=$(find "$PROTO_DIR" -maxdepth 1 -name "*.proto" -type f | wc -l)
  go_count=$(find "$GO_OUT" -maxdepth 1 -name "*.pb.go" -type f 2>/dev/null | wc -l)

  for proto in "$PROTO_DIR"/*.proto; do
    local base
    base=$(basename "$proto" .proto)
    # Go: accept auto-generated .pb.go OR hand-written .go in parent dir.
    if [ ! -f "$GO_OUT/${base}.pb.go" ] && [ ! -f "$go_parent_dir/${base}.go" ]; then
      missing+=("Go: ${base}.pb.go (or ${base}.go)")
    fi
  done

  local go_hand_count
  go_hand_count=0
  for proto in "$PROTO_DIR"/*.proto; do
    local base
    base=$(basename "$proto" .proto)
    if [ ! -f "$GO_OUT/${base}.pb.go" ] && [ -f "$go_parent_dir/${base}.go" ]; then
      go_hand_count=$((go_hand_count + 1))
    fi
  done

  if [ ${#missing[@]} -gt 0 ]; then
    warn "Proto completeness check: missing generated files:"
    for m in "${missing[@]}"; do
      warn "  - $m"
    done
    fail "Not all .proto files have generated outputs. Found $proto_count protos, $go_count Go gen + $go_hand_count Go hand-written."
  fi
  info "Proto completeness: $proto_count protos → $go_count Go gen + $go_hand_count Go hand-written (all present)"
}

clean_generated() {
  local dir="$1"
  if [ -d "$dir" ]; then
    find "$dir" -type f -name "*.pb.go" -delete
    find "$dir" -mindepth 1 -type d -empty -delete 2>/dev/null || true
  fi
}

# --- Generators ---

gen_go() {
  info "Generating Go → gateway-go/pkg/protocol/gen/"
  command -v protoc-gen-go &>/dev/null || fail "protoc-gen-go not found. Install: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
  clean_generated "$GO_OUT"
  ( mkdir -p "$GO_OUT" && cd "$PROTO_DIR" && buf generate --template buf.gen.go.yaml )
  verify_output "$GO_OUT" "*.pb.go" "Go"
}

gen_rust() {
  info "Generating Rust via prost-build (output in cargo OUT_DIR)"
  local output
  if ! output=$(cd "$REPO_ROOT/core-rs" && cargo check 2>&1); then
    echo "$output" >&2
    fail "Rust protobuf generation failed (see cargo output above)"
  fi
  info "Rust generation complete"
}

# Run Go and Rust generation in parallel.
gen_all_parallel() {
  check_prereqs buf rust
  info "Linting proto files..."
  (cd "$PROTO_DIR" && buf lint) || fail "buf lint failed — fix proto errors before generating"

  local go_log rust_log
  go_log=$(mktemp) rust_log=$(mktemp)

  # Launch both in parallel.
  gen_go   > "$go_log"   2>&1 &
  local go_pid=$!
  gen_rust > "$rust_log"  2>&1 &
  local rust_pid=$!

  local failures=()

  wait "$go_pid"   || failures+=("Go")
  wait "$rust_pid"  || failures+=("Rust")

  # Print output (success and failure both).
  cat "$go_log" "$rust_log"
  rm -f "$go_log" "$rust_log"

  if [ ${#failures[@]} -gt 0 ]; then
    fail "Generation failed for: ${failures[*]}"
  fi

  # Only save the hash after both generators succeeded. Guarded explicitly here
  # (fail() already exits the script) to make the success invariant clear and
  # resistant to future refactors that might change the error-handling path.
  save_hash
}

check_diffs() {
  verify_completeness
  info "Checking for uncommitted generated diffs..."
  cd "$REPO_ROOT"
  local paths=(
    "gateway-go/pkg/protocol/gen"
  )
  local has_diff=0

  if ! git diff --exit-code -- "${paths[@]}" >/dev/null 2>&1; then
    has_diff=1
  fi
  if ! git diff --cached --exit-code -- "${paths[@]}" >/dev/null 2>&1; then
    has_diff=1
  fi
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

# --- Standalone commands (no lock needed) ---

lint_only() {
  check_prereqs buf
  info "Linting proto files..."
  (cd "$PROTO_DIR" && buf lint) || fail "buf lint failed"
  info "Lint passed"
}

watch_protos() {
  local watch_cmd=""
  if command -v fswatch &>/dev/null; then
    watch_cmd="fswatch"
  elif command -v inotifywait &>/dev/null; then
    watch_cmd="inotifywait"
  else
    fail "Neither fswatch nor inotifywait found. Install: brew install fswatch / apt install inotify-tools"
  fi

  info "Watching $PROTO_DIR for changes (Ctrl+C to stop)..."
  (gen_all_parallel) || warn "Initial generation failed — watching for changes anyway"

  if [ "$watch_cmd" = "fswatch" ]; then
    fswatch -0 --event Updated --include '\.proto$' --exclude '.*' "$PROTO_DIR" | while read -r -d '' _; do
      info "Change detected — regenerating..."
      (gen_all_parallel) 2>&1 || warn "Generation failed (will retry on next change)"
    done
  else
    while true; do
      inotifywait -q -e modify,create,delete "$PROTO_DIR" --include '\.proto$' >/dev/null 2>&1
      info "Change detected — regenerating..."
      (gen_all_parallel) 2>&1 || warn "Generation failed (will retry on next change)"
    done
  fi
}

# --- Main ---

# --lint and --watch don't need the concurrency lock.
case "${1:-all}" in
  --lint)
    lint_only
    exit 0
    ;;
  --watch)
    watch_protos
    exit 0
    ;;
esac

acquire_lock
trap 'cleanup_on_signal' INT TERM
trap 'release_lock' EXIT

case "${1:-all}" in
  --go)
    check_prereqs buf
    gen_go
    ;;
  --rust)
    check_prereqs "" rust
    gen_rust
    ;;
  --check)
    if is_up_to_date; then
      info "Proto hash unchanged — skipping regeneration"
    else
      gen_all_parallel
    fi
    check_diffs
    ;;
  all|"")
    gen_all_parallel
    ;;
  *)
    fail "Unknown option: $1. Use --go, --rust, --check, --lint, --watch, or no args."
    ;;
esac
