#!/usr/bin/env bash
# observe.sh — unified observation CLI for a coding agent.
#
# One entry point to watch a running Deneb gateway's logging, runtime behavior,
# and per-run turn shape — the external adapter over the miniapp.observe.* RPC
# surface (see gateway-go/internal/runtime/observe). Because observe.* itself is
# in-process only, this talks to the client-token-gated miniapp.observe.* mirror,
# which is the one wire path the broader RPC surface stays closed behind.
#
# Subcommands:
#   health                          observation-plane self-status (ring usage, 24h glance)
#   logs    [--level L] [--limit N] [--run ID] [--session S] [--contains STR] [--since MS]
#   turn    <runId>                 join one run's agentlog turn-shape + captured logs
#   behavior [--days N] [--since MS] cross-session tool usage / proactive funnel / bg jobs
#
# Endpoint + auth resolution:
#   DENEB_OBSERVE_URL    gateway base URL (default http://127.0.0.1:18789, prod)
#   DENEB_CLIENT_TOKEN   client token (else read from ~/.deneb/client_token, then
#                        the newest /tmp/deneb-*-dev-state/client_token for dev)
#
# Examples:
#   scripts/observe.sh health
#   scripts/observe.sh logs --level error --limit 20
#   scripts/observe.sh turn run_0003
#   scripts/observe.sh behavior --days 7
#   DENEB_OBSERVE_URL=http://127.0.0.1:18972 scripts/observe.sh health   # a dev gateway
set -euo pipefail

URL="${DENEB_OBSERVE_URL:-http://127.0.0.1:18789}"
TOKEN="${DENEB_CLIENT_TOKEN:-}"
if [ -z "$TOKEN" ]; then
	for f in "$HOME/.deneb/client_token" /tmp/deneb-*-dev-state/client_token; do
		if [ -f "$f" ]; then
			TOKEN="$(tr -d '\n' < "$f")"
			break
		fi
	done
fi

# show prints just the RPC payload when the call succeeded, or the whole frame
# (so the error is visible) otherwise. jq if available, else Python.
show() {
	if command -v jq >/dev/null 2>&1; then
		jq '.payload // .'
	else
		python3 -m json.tool
	fi
}

rpc() { # method, params-json
	curl -s -X POST "$URL/api/v1/miniapp/rpc" \
		-H "X-Deneb-Client-Token: $TOKEN" \
		-H 'Content-Type: application/json' \
		-d "{\"type\":\"req\",\"id\":\"obs\",\"method\":\"$1\",\"params\":${2:-\{\}}}"
}

# build_params turns --flags (and a bare first positional, treated as runId) into
# a JSON object. limit/sinceMs/days are emitted unquoted (numbers); the rest are
# strings.
build_params() {
	declare -A P
	local pos=()
	while [ $# -gt 0 ]; do
		case "$1" in
			--run) P[runId]="$2"; shift 2 ;;
			--session) P[session]="$2"; shift 2 ;;
			--level) P[level]="$2"; shift 2 ;;
			--limit) P[limit]="$2"; shift 2 ;;
			--contains) P[contains]="$2"; shift 2 ;;
			--since) P[sinceMs]="$2"; shift 2 ;;
			--days) P[days]="$2"; shift 2 ;;
			*) pos+=("$1"); shift ;;
		esac
	done
	if [ -z "${P[runId]:-}" ] && [ ${#pos[@]} -gt 0 ]; then
		P[runId]="${pos[0]}"
	fi
	local json="{" sep="" k v val
	for k in "${!P[@]}"; do
		v="${P[$k]}"
		case "$k" in
			limit|sinceMs|days) val="$v" ;;
			*) val="\"$v\"" ;;
		esac
		json="$json$sep\"$k\":$val"
		sep=","
	done
	echo "$json}"
}

cmd="${1:-health}"
shift || true

case "$cmd" in
	health)   rpc miniapp.observe.health '{}' | show ;;
	logs)     rpc miniapp.observe.logs "$(build_params "$@")" | show ;;
	turn)     rpc miniapp.observe.turn "$(build_params "$@")" | show ;;
	behavior) rpc miniapp.observe.behavior "$(build_params "$@")" | show ;;
	-h|--help|help)
		grep '^#' "$0" | sed 's/^# \{0,1\}//'
		;;
	*)
		echo "usage: observe.sh {health|logs|turn <runId>|behavior} [flags]" >&2
		echo "       flags: --run --session --level --limit --contains --since --days" >&2
		exit 2
		;;
esac
