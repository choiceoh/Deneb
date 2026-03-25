#!/usr/bin/env bash
# DenebDock - Docker helpers for Deneb
# Inspired by Simon Willison's "Running Deneb in Docker"
# https://til.simonwillison.net/llms/deneb-docker
#
# Installation:
#   mkdir -p ~/.deneb-dock && curl -sL https://raw.githubusercontent.com/deneb/deneb/main/scripts/shell-helpers/deneb-dock-helpers.sh -o ~/.deneb-dock/deneb-dock-helpers.sh
#   echo 'source ~/.deneb-dock/deneb-dock-helpers.sh' >> ~/.zshrc
#
# Usage:
#   deneb-dock-help    # Show all available commands

# =============================================================================
# Colors
# =============================================================================
_CLR_RESET='\033[0m'
_CLR_BOLD='\033[1m'
_CLR_DIM='\033[2m'
_CLR_GREEN='\033[0;32m'
_CLR_YELLOW='\033[1;33m'
_CLR_BLUE='\033[0;34m'
_CLR_MAGENTA='\033[0;35m'
_CLR_CYAN='\033[0;36m'
_CLR_RED='\033[0;31m'

# Styled command output (green + bold)
_clr_cmd() {
  echo -e "${_CLR_GREEN}${_CLR_BOLD}$1${_CLR_RESET}"
}

# Inline command for use in sentences
_cmd() {
  echo "${_CLR_GREEN}${_CLR_BOLD}$1${_CLR_RESET}"
}

# =============================================================================
# Config
# =============================================================================
DENEB_DOCK_CONFIG="${HOME}/.deneb-dock/config"

# Common paths to check for Deneb
DENEB_DOCK_COMMON_PATHS=(
  "${HOME}/deneb"
  "${HOME}/workspace/deneb"
  "${HOME}/projects/deneb"
  "${HOME}/dev/deneb"
  "${HOME}/code/deneb"
  "${HOME}/src/deneb"
)

_deneb-dock_filter_warnings() {
  grep -v "^WARN\|^time="
}

_deneb-dock_trim_quotes() {
  local value="$1"
  value="${value#\"}"
  value="${value%\"}"
  printf "%s" "$value"
}

_deneb-dock_read_config_dir() {
  if [[ ! -f "$DENEB_DOCK_CONFIG" ]]; then
    return 1
  fi
  local raw
  raw=$(sed -n 's/^DENEB_DOCK_DIR=//p' "$DENEB_DOCK_CONFIG" | head -n 1)
  if [[ -z "$raw" ]]; then
    return 1
  fi
  _deneb-dock_trim_quotes "$raw"
}

# Ensure DENEB_DOCK_DIR is set and valid
_deneb-dock_ensure_dir() {
  # Already set and valid?
  if [[ -n "$DENEB_DOCK_DIR" && -f "${DENEB_DOCK_DIR}/docker-compose.yml" ]]; then
    return 0
  fi

  # Try loading from config
  local config_dir
  config_dir=$(_deneb-dock_read_config_dir)
  if [[ -n "$config_dir" && -f "${config_dir}/docker-compose.yml" ]]; then
    DENEB_DOCK_DIR="$config_dir"
    return 0
  fi

  # Auto-detect from common paths
  local found_path=""
  for path in "${DENEB_DOCK_COMMON_PATHS[@]}"; do
    if [[ -f "${path}/docker-compose.yml" ]]; then
      found_path="$path"
      break
    fi
  done

  if [[ -n "$found_path" ]]; then
    echo ""
    echo "🦞 Found Deneb at: $found_path"
    echo -n "   Use this location? [Y/n] "
    read -r response
    if [[ "$response" =~ ^[Nn] ]]; then
      echo ""
      echo "Set DENEB_DOCK_DIR manually:"
      echo "  export DENEB_DOCK_DIR=/path/to/deneb"
      return 1
    fi
    DENEB_DOCK_DIR="$found_path"
  else
    echo ""
    echo "❌ Deneb not found in common locations."
    echo ""
    echo "Clone it first:"
    echo ""
    echo "  git clone https://github.com/deneb/deneb.git ~/deneb"
    echo "  cd ~/deneb && ./docker-setup.sh"
    echo ""
    echo "Or set DENEB_DOCK_DIR if it's elsewhere:"
    echo ""
    echo "  export DENEB_DOCK_DIR=/path/to/deneb"
    echo ""
    return 1
  fi

  # Save to config
  if [[ ! -d "${HOME}/.deneb-dock" ]]; then
    /bin/mkdir -p "${HOME}/.deneb-dock"
  fi
  echo "DENEB_DOCK_DIR=\"$DENEB_DOCK_DIR\"" > "$DENEB_DOCK_CONFIG"
  echo "✅ Saved to $DENEB_DOCK_CONFIG"
  echo ""
  return 0
}

# Wrapper to run docker compose commands
_deneb-dock_compose() {
  _deneb-dock_ensure_dir || return 1
  local compose_args=(-f "${DENEB_DOCK_DIR}/docker-compose.yml")
  if [[ -f "${DENEB_DOCK_DIR}/docker-compose.extra.yml" ]]; then
    compose_args+=(-f "${DENEB_DOCK_DIR}/docker-compose.extra.yml")
  fi
  command docker compose "${compose_args[@]}" "$@"
}

_deneb-dock_read_env_token() {
  _deneb-dock_ensure_dir || return 1
  if [[ ! -f "${DENEB_DOCK_DIR}/.env" ]]; then
    return 1
  fi
  local raw
  raw=$(sed -n 's/^DENEB_GATEWAY_TOKEN=//p' "${DENEB_DOCK_DIR}/.env" | head -n 1)
  if [[ -z "$raw" ]]; then
    return 1
  fi
  _deneb-dock_trim_quotes "$raw"
}

# Basic Operations
deneb-dock-start() {
  _deneb-dock_compose up -d deneb-gateway
}

deneb-dock-stop() {
  _deneb-dock_compose down
}

deneb-dock-restart() {
  _deneb-dock_compose restart deneb-gateway
}

deneb-dock-logs() {
  _deneb-dock_compose logs -f deneb-gateway
}

deneb-dock-status() {
  _deneb-dock_compose ps
}

# Navigation
deneb-dock-cd() {
  _deneb-dock_ensure_dir || return 1
  cd "${DENEB_DOCK_DIR}"
}

deneb-dock-config() {
  cd ~/.deneb
}

deneb-dock-workspace() {
  cd ~/.deneb/workspace
}

# Container Access
deneb-dock-shell() {
  _deneb-dock_compose exec deneb-gateway \
    bash -c 'echo "alias deneb=\"./deneb.mjs\"" > /tmp/.bashrc_deneb && bash --rcfile /tmp/.bashrc_deneb'
}

deneb-dock-exec() {
  _deneb-dock_compose exec deneb-gateway "$@"
}

deneb-dock-cli() {
  _deneb-dock_compose run --rm deneb-cli "$@"
}

# Maintenance
deneb-dock-rebuild() {
  _deneb-dock_compose build deneb-gateway
}

deneb-dock-clean() {
  _deneb-dock_compose down -v --remove-orphans
}

# Health check
deneb-dock-health() {
  _deneb-dock_ensure_dir || return 1
  local token
  token=$(_deneb-dock_read_env_token)
  if [[ -z "$token" ]]; then
    echo "❌ Error: Could not find gateway token"
    echo "   Check: ${DENEB_DOCK_DIR}/.env"
    return 1
  fi
  _deneb-dock_compose exec -e "DENEB_GATEWAY_TOKEN=$token" deneb-gateway \
    node dist/index.js health
}

# Show gateway token
deneb-dock-token() {
  _deneb-dock_read_env_token
}

# Fix token configuration (run this once after setup)
deneb-dock-fix-token() {
  _deneb-dock_ensure_dir || return 1

  echo "🔧 Configuring gateway token..."
  local token
  token=$(deneb-dock-token)
  if [[ -z "$token" ]]; then
    echo "❌ Error: Could not find gateway token"
    echo "   Check: ${DENEB_DOCK_DIR}/.env"
    return 1
  fi

  echo "📝 Setting token: ${token:0:20}..."

  _deneb-dock_compose exec -e "TOKEN=$token" deneb-gateway \
    bash -c './deneb.mjs config set gateway.remote.token "$TOKEN" && ./deneb.mjs config set gateway.auth.token "$TOKEN"' 2>&1 | _deneb-dock_filter_warnings

  echo "🔍 Verifying token was saved..."
  local saved_token
  saved_token=$(_deneb-dock_compose exec deneb-gateway \
    bash -c "./deneb.mjs config get gateway.remote.token 2>/dev/null" 2>&1 | _deneb-dock_filter_warnings | tr -d '\r\n' | head -c 64)

  if [[ "$saved_token" == "$token" ]]; then
    echo "✅ Token saved correctly!"
  else
    echo "⚠️  Token mismatch detected"
    echo "   Expected: ${token:0:20}..."
    echo "   Got: ${saved_token:0:20}..."
  fi

  echo "🔄 Restarting gateway..."
  _deneb-dock_compose restart deneb-gateway 2>&1 | _deneb-dock_filter_warnings

  echo "⏳ Waiting for gateway to start..."
  sleep 5

  echo "✅ Configuration complete!"
  echo -e "   Try: $(_cmd deneb-dock-devices)"
}

# Open dashboard in browser
deneb-dock-dashboard() {
  _deneb-dock_ensure_dir || return 1

  echo "🦞 Getting dashboard URL..."
  local output exit_status url
  output=$(_deneb-dock_compose run --rm deneb-cli dashboard --no-open 2>&1)
  exit_status=$?
  url=$(printf "%s\n" "$output" | _deneb-dock_filter_warnings | grep -o 'http[s]\?://[^[:space:]]*' | head -n 1)
  if [[ $exit_status -ne 0 ]]; then
    echo "❌ Failed to get dashboard URL"
    echo -e "   Try restarting: $(_cmd deneb-dock-restart)"
    return 1
  fi

  if [[ -n "$url" ]]; then
    echo "✅ Opening: $url"
    open "$url" 2>/dev/null || xdg-open "$url" 2>/dev/null || echo "   Please open manually: $url"
    echo ""
    echo -e "${_CLR_CYAN}💡 If you see 'pairing required' error:${_CLR_RESET}"
    echo -e "   1. Run: $(_cmd deneb-dock-devices)"
    echo "   2. Copy the Request ID from the Pending table"
    echo -e "   3. Run: $(_cmd 'deneb-dock-approve <request-id>')"
  else
    echo "❌ Failed to get dashboard URL"
    echo -e "   Try restarting: $(_cmd deneb-dock-restart)"
  fi
}

# List device pairings
deneb-dock-devices() {
  _deneb-dock_ensure_dir || return 1

  echo "🔍 Checking device pairings..."
  local output exit_status
  output=$(_deneb-dock_compose exec deneb-gateway node dist/index.js devices list 2>&1)
  exit_status=$?
  printf "%s\n" "$output" | _deneb-dock_filter_warnings
  if [ $exit_status -ne 0 ]; then
    echo ""
    echo -e "${_CLR_CYAN}💡 If you see token errors above:${_CLR_RESET}"
    echo -e "   1. Verify token is set: $(_cmd deneb-dock-token)"
    echo "   2. Try manual config inside container:"
    echo -e "      $(_cmd deneb-dock-shell)"
    echo -e "      $(_cmd 'deneb config get gateway.remote.token')"
    return 1
  fi

  echo ""
  echo -e "${_CLR_CYAN}💡 To approve a pairing request:${_CLR_RESET}"
  echo -e "   $(_cmd 'deneb-dock-approve <request-id>')"
}

# Approve device pairing request
deneb-dock-approve() {
  _deneb-dock_ensure_dir || return 1

  if [[ -z "$1" ]]; then
    echo -e "❌ Usage: $(_cmd 'deneb-dock-approve <request-id>')"
    echo ""
    echo -e "${_CLR_CYAN}💡 How to approve a device:${_CLR_RESET}"
    echo -e "   1. Run: $(_cmd deneb-dock-devices)"
    echo "   2. Find the Request ID in the Pending table (long UUID)"
    echo -e "   3. Run: $(_cmd 'deneb-dock-approve <that-request-id>')"
    echo ""
    echo "Example:"
    echo -e "   $(_cmd 'deneb-dock-approve 6f9db1bd-a1cc-4d3f-b643-2c195262464e')"
    return 1
  fi

  echo "✅ Approving device: $1"
  _deneb-dock_compose exec deneb-gateway \
    node dist/index.js devices approve "$1" 2>&1 | _deneb-dock_filter_warnings

  echo ""
  echo "✅ Device approved! Refresh your browser."
}

# Show all available deneb-dock helper commands
deneb-dock-help() {
  echo -e "\n${_CLR_BOLD}${_CLR_CYAN}🦞 DenebDock - Docker Helpers for Deneb${_CLR_RESET}\n"

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}⚡ Basic Operations${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-start)       ${_CLR_DIM}Start the gateway${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-stop)        ${_CLR_DIM}Stop the gateway${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-restart)     ${_CLR_DIM}Restart the gateway${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-status)      ${_CLR_DIM}Check container status${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-logs)        ${_CLR_DIM}View live logs (follows)${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}🐚 Container Access${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-shell)       ${_CLR_DIM}Shell into container (deneb alias ready)${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-cli)         ${_CLR_DIM}Run CLI commands (e.g., deneb-dock-cli status)${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-exec) ${_CLR_CYAN}<cmd>${_CLR_RESET}  ${_CLR_DIM}Execute command in gateway container${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}🌐 Web UI & Devices${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-dashboard)   ${_CLR_DIM}Open web UI in browser ${_CLR_CYAN}(auto-guides you)${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-devices)     ${_CLR_DIM}List device pairings ${_CLR_CYAN}(auto-guides you)${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-approve) ${_CLR_CYAN}<id>${_CLR_RESET} ${_CLR_DIM}Approve device pairing ${_CLR_CYAN}(with examples)${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}⚙️  Setup & Configuration${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-fix-token)   ${_CLR_DIM}Configure gateway token ${_CLR_CYAN}(run once)${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}🔧 Maintenance${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-rebuild)     ${_CLR_DIM}Rebuild Docker image${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-clean)       ${_CLR_RED}⚠️  Remove containers & volumes (nuclear)${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_MAGENTA}🛠️  Utilities${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-health)      ${_CLR_DIM}Run health check${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-token)       ${_CLR_DIM}Show gateway auth token${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-cd)          ${_CLR_DIM}Jump to deneb project directory${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-config)      ${_CLR_DIM}Open config directory (~/.deneb)${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-workspace)   ${_CLR_DIM}Open workspace directory${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${_CLR_RESET}"
  echo -e "${_CLR_BOLD}${_CLR_GREEN}🚀 First Time Setup${_CLR_RESET}"
  echo -e "${_CLR_CYAN}  1.${_CLR_RESET} $(_cmd deneb-dock-start)          ${_CLR_DIM}# Start the gateway${_CLR_RESET}"
  echo -e "${_CLR_CYAN}  2.${_CLR_RESET} $(_cmd deneb-dock-fix-token)      ${_CLR_DIM}# Configure token${_CLR_RESET}"
  echo -e "${_CLR_CYAN}  3.${_CLR_RESET} $(_cmd deneb-dock-dashboard)      ${_CLR_DIM}# Open web UI${_CLR_RESET}"
  echo -e "${_CLR_CYAN}  4.${_CLR_RESET} $(_cmd deneb-dock-devices)        ${_CLR_DIM}# If pairing needed${_CLR_RESET}"
  echo -e "${_CLR_CYAN}  5.${_CLR_RESET} $(_cmd deneb-dock-approve) ${_CLR_CYAN}<id>${_CLR_RESET}   ${_CLR_DIM}# Approve pairing${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_GREEN}💬 WhatsApp Setup${_CLR_RESET}"
  echo -e "  $(_cmd deneb-dock-shell)"
  echo -e "    ${_CLR_BLUE}>${_CLR_RESET} $(_cmd 'deneb channels login --channel whatsapp')"
  echo -e "    ${_CLR_BLUE}>${_CLR_RESET} $(_cmd 'deneb status')"
  echo ""

  echo -e "${_CLR_BOLD}${_CLR_CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${_CLR_RESET}"
  echo ""

  echo -e "${_CLR_CYAN}💡 All commands guide you through next steps!${_CLR_RESET}"
  echo -e "${_CLR_BLUE}📚 Docs: ${_CLR_RESET}${_CLR_CYAN}https://docs.deneb.ai${_CLR_RESET}"
  echo ""
}
