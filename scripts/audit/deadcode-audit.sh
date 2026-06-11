#!/usr/bin/env bash
# deadcode-audit.sh — advisory dead-code audit for the Go gateway.
#
# Runs x/tools deadcode rooted at all gateway binaries and diffs the findings
# against a checked-in baseline, so newly-orphaned code surfaces in review
# instead of accumulating silently (the 2026-06 audit series removed ~8,700
# LOC that had built up this way: #2220 #2224 #2240).
#
# Advisory, not a make-check gate: whole-program reachability analysis takes
# ~1-2 minutes and the baseline needs human judgment — deadcode ignores
# _test.go, so a finding may still be test-reachable or a documented
# extension point (see the baseline header for the keep-policy).
#
# Usage:
#   scripts/audit/deadcode-audit.sh            # diff against baseline
#   scripts/audit/deadcode-audit.sh --update   # rewrite the baseline
#
# Exit codes: 0 = no new findings; 1 = new findings (listed on stdout);
# 2 = tooling failure.
#
# Baseline edits follow .claude/rules/testing.md: do not regenerate it to
# silence a failing diff without explicit operator approval — either delete
# the newly-dead code or get the keep approved.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
baseline="$repo_root/scripts/audit/deadcode-baseline.txt"
cd "$repo_root/gateway-go"

# Normalize to "path :: symbol" (line:col drift with unrelated edits).
current="$(go run golang.org/x/tools/cmd/deadcode@latest ./cmd/... 2>/dev/null |
    sed -E 's/^([^:]+):[0-9]+:[0-9]+: unreachable func: (.*)$/\1 :: \2/' | sort)" || {
    echo "deadcode-audit: failed to run deadcode" >&2
    exit 2
}

if [[ "${1:-}" == "--update" ]]; then
    {
        echo "# deadcode-baseline.txt — accepted findings of scripts/audit/deadcode-audit.sh."
        echo "# Format: <file> :: <symbol>, sorted. Regenerate with --update (operator approval"
        echo "# required per .claude/rules/testing.md)."
        echo "#"
        echo "# Keep-policy: entries stay here only if they are (a) reachable from _test.go"
        echo "# (deadcode cannot see tests), (b) documented extension points (e.g."
        echo "# HookCompositor.SetBeforeToolCall in gateway-go/CLAUDE.md), or (c) pending an"
        echo "# operator rewire-vs-retire decision. Anything else should be deleted instead"
        echo "# of baselined."
        echo "$current"
    } > "$baseline"
    echo "deadcode-audit: baseline updated ($(grep -vc '^#' "$baseline") entries)"
    exit 0
fi

if [[ ! -f "$baseline" ]]; then
    echo "deadcode-audit: baseline missing; run with --update first" >&2
    exit 2
fi

known="$(grep -v '^#' "$baseline" | sort)"

new_findings="$(comm -13 <(echo "$known") <(echo "$current"))"
resolved="$(comm -23 <(echo "$known") <(echo "$current"))"

if [[ -n "$resolved" ]]; then
    echo "deadcode-audit: $(echo "$resolved" | wc -l) baseline entries no longer dead (stale — refresh with --update):"
    echo "$resolved" | sed 's/^/  - /'
fi

if [[ -n "$new_findings" ]]; then
    echo "deadcode-audit: NEW dead code ($(echo "$new_findings" | wc -l) findings):"
    echo "$new_findings" | sed 's/^/  + /'
    echo "deadcode-audit: delete the code (preferred) or baseline it with operator approval."
    exit 1
fi

echo "deadcode-audit: clean ($(echo "$known" | wc -l) accepted baseline entries, 0 new)"
