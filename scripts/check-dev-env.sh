#!/usr/bin/env bash
# check-dev-env.sh — Validate development prerequisites for AI agent sessions.
# Non-blocking: prints warnings but always exits 0 so sessions can start.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MISSING=0

# Mirror the Makefile's PATH (Makefile:29) so tool detection matches how the
# build actually resolves Go-installed binaries like golangci-lint.
export PATH="$HOME/go/bin:$PATH"

check_tool() {
    local name="$1"
    local cmd="$2"
    local hint="$3"

    if command -v "$cmd" &>/dev/null; then
        local ver
        # `|| true` swallows the SIGPIPE-induced non-zero that pipefail raises
        # when head closes a multi-line --version early (else ver double-prints).
        ver=$("$cmd" --version 2>/dev/null | head -1) || true
        if [ -z "$ver" ]; then ver=$("$cmd" version 2>/dev/null | head -1) || true; fi
        if [ -z "$ver" ]; then ver="(installed)"; fi
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
check_tool "make" "make" "apt-get install make (drives every build/test target)"
check_tool "golangci-lint" "golangci-lint" "make check / make go-lint need it: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
check_tool "ripgrep" "rg" "apt-get install ripgrep"
check_tool "python3" "python3" "apt-get install python3 (live-test quality + iterate scripts)"

echo ""
echo "Go modules:"
if (cd "$REPO_ROOT/gateway-go" && go mod verify) &>/dev/null 2>&1; then
    echo "  [ok] Go module cache verified"
else
    echo "  [missing] Go modules not cached — run 'cd gateway-go && go mod download'"
    MISSING=$((MISSING + 1))
fi

echo ""
echo "Build memory headroom (DGX unified memory — GPU shares system RAM):"
if [ -r /proc/meminfo ]; then
    # Mirrors the Makefile GO_PAR formula (~4 GB budgeted per parallel action,
    # clamped to [2, NPROC]) so the developer sees what `make` will pick.
    mem_gb=$(awk '/MemAvailable/ {printf "%d", $2/1024/1024}' /proc/meminfo)
    mem_gb=${mem_gb:-0}
    cpu=$(nproc 2>/dev/null || echo 4)
    par=$(( mem_gb / 4 ))
    if [ "$par" -gt "$cpu" ]; then par=$cpu; fi
    if [ "$par" -lt 2 ]; then par=2; fi
    if [ "$mem_gb" -lt 8 ]; then
        echo "  [warn] only ${mem_gb} GB free — make throttles to GO_PAR=${par}; export GOGC=50 if builds still OOM"
    else
        echo "  [ok] ${mem_gb} GB free (make auto-selects GO_PAR=${par} parallel build/test actions)"
    fi
else
    echo "  [skip] /proc/meminfo unavailable (non-Linux?)"
fi

echo ""
echo "Multi-agent git tooling (optional accelerators — hints only, never fatal):"
if command -v mergiraf >/dev/null 2>&1; then
    if [ "$(git config --global --get merge.mergiraf.name 2>/dev/null)" = "mergiraf" ]; then
        echo "  [ok] mergiraf + merge driver configured (structural merges per .gitattributes)"
    else
        echo "  [hint] mergiraf installed but driver unset — see setup comment in .gitattributes"
    fi
else
    echo "  [hint] mergiraf not installed — .gitattributes merge=mergiraf falls back to text merge"
fi
if [ "$(git config --global --get rerere.enabled 2>/dev/null)" = "true" ]; then
    echo "  [ok] git rerere enabled (conflict resolutions reused across worktrees)"
else
    echo "  [hint] enable rerere: git config --global rerere.enabled true"
fi
if [ -n "$(git config --local --get maintenance.strategy 2>/dev/null)" ]; then
    echo "  [ok] git maintenance registered (background commit-graph/prefetch)"
else
    echo "  [hint] register maintenance: git maintenance start (once, in the main checkout)"
fi
if command -v clash >/dev/null 2>&1; then
    echo "  [ok] clash installed (cross-worktree conflict early warning: clash status)"
else
    echo "  [hint] clash not installed — worktree conflict scanner (github.com/clash-sh/clash)"
fi

echo ""

if [ "$MISSING" -gt 0 ]; then
    echo "Warning: $MISSING tool(s) missing. Some build targets may fail."
    echo "See CLAUDE.md Git/PR + Makefile `info` targets for setup instructions."
else
    echo "All tools available. Ready to build."
fi

echo ""
echo "Build: make go -> make test"
echo "Fast iteration: make go-dev"

# Always exit 0 so the session can start regardless.
exit 0
