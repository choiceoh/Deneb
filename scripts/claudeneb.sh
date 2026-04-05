#!/usr/bin/env bash
# Claudeneb — Claude Desktop + Deneb integration launcher.
#
# Two modes:
#   1. Anthropic mode (default): Claude Desktop OAuth → Deneb proxy → Anthropic API
#   2. OpenAI mode (--openai): No Anthropic key needed. Uses localai/z.ai/vLLM.
#
# Usage:
#   ./scripts/claudeneb.sh                # Anthropic mode (uses Claude Desktop's own login)
#   ./scripts/claudeneb.sh --openai       # OpenAI-compatible mode (localai/z.ai)
#   ./scripts/claudeneb.sh --check        # Check prerequisites only

set -euo pipefail

DENEB_GATEWAY_URL="http://127.0.0.1:18789"
DENEB_GATEWAY_PORT=18789

red='\033[0;31m'; green='\033[0;32m'; yellow='\033[0;33m'; bold='\033[1m'; reset='\033[0m'
pass() { echo -e "${green}[OK]${reset} $1"; }
fail() { echo -e "${red}[FAIL]${reset} $1"; }
warn() { echo -e "${yellow}[WARN]${reset} $1"; }

USE_OPENAI=false
[[ "${1:-}" == "--openai" ]] && USE_OPENAI=true && shift

check_prerequisites() {
    local errors=0

    # Deneb gateway running?
    if curl -sf "${DENEB_GATEWAY_URL}/health" >/dev/null 2>&1; then
        pass "Deneb gateway running on port ${DENEB_GATEWAY_PORT}"
    else
        fail "Deneb gateway not running on port ${DENEB_GATEWAY_PORT}"
        errors=$((errors + 1))
    fi

    # /v1/messages endpoint enabled?
    local status
    status=$(curl -sf -o /dev/null -w "%{http_code}" \
        -X POST "${DENEB_GATEWAY_URL}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: test" \
        -d '{}' 2>/dev/null) || status="000"
    if [[ "$status" == "400" ]]; then
        pass "/v1/messages endpoint enabled"
    elif [[ "$status" == "404" ]]; then
        fail "/v1/messages endpoint not enabled"
        echo "     Fix: set gateway.http.endpoints.anthropicMessages.enabled=true"
        errors=$((errors + 1))
    else
        warn "/v1/messages returned status ${status}"
    fi

    # Mode-specific checks.
    if [[ "$USE_OPENAI" == true ]]; then
        if [[ -n "${CLAUDENEB_OPENAI_URL:-}" ]]; then
            pass "CLAUDENEB_OPENAI_URL = ${CLAUDENEB_OPENAI_URL}"
        else
            # Default to local AI.
            warn "CLAUDENEB_OPENAI_URL not set, will default to http://127.0.0.1:30000/v1"
        fi
        pass "OpenAI mode — no Anthropic API key needed"
    else
        if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
            pass "ANTHROPIC_API_KEY set (server-side override)"
        else
            pass "Anthropic mode — will use Claude Desktop's own OAuth token"
        fi
    fi

    # Claude Desktop installed?
    if command -v claude-desktop &>/dev/null; then
        pass "Claude Desktop installed"
    else
        fail "Claude Desktop not installed"
        errors=$((errors + 1))
    fi

    return $errors
}

if [[ "${1:-}" == "--check" ]]; then
    echo -e "${bold}Claudeneb Prerequisites Check${reset}"
    echo "================================"
    if check_prerequisites; then
        echo -e "\n${green}${bold}Ready to launch.${reset}"
    else
        echo -e "\n${red}${bold}Fix the issues above first.${reset}"
        exit 1
    fi
    exit 0
fi

echo -e "${bold}Claudeneb${reset} — Claude Desktop + Deneb"
echo ""
if ! check_prerequisites; then
    echo ""
    fail "Prerequisites not met."
    exit 1
fi
echo ""

# Set environment for Claude Desktop.
export ANTHROPIC_BASE_URL="${DENEB_GATEWAY_URL}"

if [[ "$USE_OPENAI" == true ]]; then
    export CLAUDENEB_OPENAI_URL="${CLAUDENEB_OPENAI_URL:-http://127.0.0.1:30000/v1}"
    echo -e "${bold}Mode:${reset} OpenAI-compatible (${CLAUDENEB_OPENAI_URL})"
else
    echo -e "${bold}Mode:${reset} Anthropic passthrough"
fi

echo -e "${bold}Launching...${reset}"
exec claude-desktop "$@"
