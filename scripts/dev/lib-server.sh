#!/usr/bin/env bash
# lib-server.sh — Shared dev server management library.
#
# Source this file from dev-*.sh scripts:
#   source "$(dirname "$0")/lib-server.sh"
#
# Provides:
#   devlib_load_dotenv                 Load ~/.deneb/.env
#   devlib_version                     Get deneb version from git tags
#   devlib_build BINARY [REPO]         Build gateway binary
#   devlib_gen_config OUT [TOKEN]      Generate dev config
#   devlib_start_gateway BIN PORT CFG STATE LOG [nohup]
#   devlib_wait_healthy HOST PORT [MAX]
#   devlib_stop_pid PID [TIMEOUT_DS]
#   devlib_wait_port_free PORT [MAX]
#   devlib_is_pid_alive PID
#
# Constants (set after sourcing):
#   DEVLIB_SCRIPT_DIR    Directory containing this library
#   DEVLIB_REPO_DIR      Repository root
#   DEVLIB_HOST          Loopback address (127.0.0.1)

# Guard against double-sourcing.
[[ -n "${_DEVLIB_LOADED:-}" ]] && return 0
_DEVLIB_LOADED=1

DEVLIB_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEVLIB_REPO_DIR="$(cd "$DEVLIB_SCRIPT_DIR/.." && pwd)"
DEVLIB_HOST="127.0.0.1"

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------

# Load ~/.deneb/.env without overriding existing variables.
devlib_load_dotenv() {
  local dotenv="${HOME}/.deneb/.env"
  [[ -f "$dotenv" ]] || return 0
  local key val
  while IFS='=' read -r key val; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    key="${key## }"; key="${key%% }"
    val="${val## }"; val="${val%% }"
    val="${val#\"}"; val="${val%\"}"
    val="${val#\'}"; val="${val%\'}"
    if [[ -z "${!key:-}" ]]; then
      export "$key=$val"
    fi
  done < "$dotenv"
}

# Get version string from the latest deneb-v* git tag.
devlib_version() {
  git -C "$DEVLIB_REPO_DIR" tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null \
    | head -1 | sed 's/^deneb-v//'
}

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

# Build gateway binary.
#   $1 — output binary path (required)
#   $2 — repo dir override (optional, defaults to DEVLIB_REPO_DIR)
devlib_build() {
  local binary="$1"
  local repo="${2:-$DEVLIB_REPO_DIR}"
  local version
  version=$(devlib_version)
  go build -C "$repo/gateway-go" \
    -ldflags "-s -w -X main.Version=${version:-dev}" \
    -o "$binary" ./cmd/gateway/
}

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

# Generate dev config via config-gen.sh.
#   $1 — output path (required)
#   $2 — Telegram token override (optional)
devlib_gen_config() {
  local out="$1"
  if [[ -n "${2:-}" ]]; then
    DENEB_DEV_TELEGRAM_TOKEN="$2" \
      "$DEVLIB_SCRIPT_DIR/config-gen.sh" --out "$out" >/dev/null 2>&1
  else
    "$DEVLIB_SCRIPT_DIR/config-gen.sh" --out "$out" >/dev/null 2>&1
  fi
}

# ---------------------------------------------------------------------------
# Server lifecycle
# ---------------------------------------------------------------------------

# Start gateway process in background.
# Sets DEVLIB_PID to the started process PID.
#   $1 — binary path
#   $2 — port
#   $3 — config path
#   $4 — state dir (wiki/diary isolation)
#   $5 — log file path
#   $6 — "nohup" to survive terminal close (optional)
devlib_start_gateway() {
  local binary="$1" port="$2" config="$3" state_dir="$4" log="$5"
  local use_nohup="${6:-}"

  mkdir -p "$state_dir"

  if [[ "$use_nohup" == "nohup" ]]; then
    DENEB_CONFIG_PATH="$config" \
    DENEB_STATE_DIR="$state_dir" \
    DENEB_WIKI_DIR="$state_dir/wiki" \
    DENEB_WIKI_DIARY_DIR="$state_dir/memory/diary" \
    nohup "$binary" --bind loopback --port "$port" > "$log" 2>&1 &
  else
    DENEB_CONFIG_PATH="$config" \
    DENEB_STATE_DIR="$state_dir" \
    DENEB_WIKI_DIR="$state_dir/wiki" \
    DENEB_WIKI_DIARY_DIR="$state_dir/memory/diary" \
    "$binary" --bind loopback --port "$port" > "$log" 2>&1 &
  fi
  DEVLIB_PID=$!
}

# Wait for /health to respond OK (exponential backoff: 50ms -> 300ms cap).
# Exits early if DEVLIB_PID is set and process dies.
#   $1 — host
#   $2 — port
#   $3 — max retries (optional, default 25 ~ 6s)
# Returns: 0 on healthy, 1 on timeout/crash.
devlib_wait_healthy() {
  local host="$1" port="$2" max_retries="${3:-25}"
  local retries=0 wait_ms=50
  while (( retries < max_retries )); do
    if curl -sf "http://$host:$port/health" > /dev/null 2>&1; then
      return 0
    fi
    if [[ -n "${DEVLIB_PID:-}" ]] && ! kill -0 "$DEVLIB_PID" 2>/dev/null; then
      return 1
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $wait_ms/1000}")"
    wait_ms=$(( wait_ms * 2 )); (( wait_ms > 300 )) && wait_ms=300
    retries=$((retries + 1))
  done
  return 1
}

# Gracefully stop a process: SIGTERM -> wait -> SIGKILL fallback.
#   $1 — PID
#   $2 — timeout in deciseconds (optional, default 30 = 3s)
devlib_stop_pid() {
  local pid="$1" timeout="${2:-30}"
  [[ -n "$pid" ]] || return 0
  kill "$pid" 2>/dev/null || return 0
  local waited=0
  while kill -0 "$pid" 2>/dev/null && (( waited < timeout )); do
    sleep 0.1
    waited=$((waited + 1))
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}

# Wait for a TCP port to become free (exponential backoff: 30ms -> 200ms cap).
#   $1 — port
#   $2 — max retries (optional, default 15 ~ 3s)
# Returns: 0 if free, 1 if still held.
devlib_wait_port_free() {
  local port="$1" max_retries="${2:-15}"
  local retries=0 wait_ms=30
  while (( retries < max_retries )); do
    if ! ss -ltnp 2>/dev/null | grep -q ":$port "; then
      return 0
    fi
    sleep "$(awk "BEGIN {printf \"%.3f\", $wait_ms/1000}")"
    wait_ms=$(( wait_ms * 2 )); (( wait_ms > 200 )) && wait_ms=200
    retries=$((retries + 1))
  done
  return 1
}

# Check if a PID is alive.
devlib_is_pid_alive() {
  [[ -n "${1:-}" ]] && kill -0 "$1" 2>/dev/null
}
