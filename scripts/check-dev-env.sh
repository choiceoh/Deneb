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
check_tool "Go" "go" "Install Go 1.24+ from https://go.dev/dl"
check_tool "ripgrep" "rg" "apt-get install ripgrep"

echo ""
echo "Go modules:"
if (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then
    echo "  [ok] Go module cache verified"
else
    echo "  [missing] Go modules not cached — run 'cd gateway-go && go mod download'"
    MISSING=$((MISSING + 1))
fi

echo ""

if [ "$MISSING" -gt 0 ]; then
    echo "Warning: $MISSING tool(s) missing. Some build targets may fail."
    echo "See CLAUDE.md 'Agent Quick-Start' section for setup instructions."
else
    echo "All tools available. Ready to build."
fi

echo ""
echo "Build: make go -> make test"
echo "Fast iteration: make go-dev"

# Always exit 0 so the session can start regardless.
exit 0
