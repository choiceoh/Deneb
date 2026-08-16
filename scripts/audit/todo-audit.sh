#!/usr/bin/env bash
# todo-audit.sh — advisory TODO/FIXME/HACK/XXX ratchet for the whole repo.
#
# Counts tech-debt markers (TODO/FIXME/HACK/XXX) across tracked code files and
# diffs them against a checked-in baseline, so new markers surface in review
# instead of accumulating silently. Sibling to scripts/audit/deadcode-audit.sh:
# same "advisory + baseline diff" shape, same exit-code contract.
#
# Advisory, NOT part of `make check`/`ci`: a TODO is often a deliberate
# placeholder, so triage needs human judgment. The ratchet is directional —
# adding a new marker requires either deleting it or removing an existing one
# (or updating the baseline with operator approval). Moving a marker within a
# file does NOT count as new (line numbers are stripped from the normalized
# form).
#
# Scope: tracked files with extensions .go .kt .ts .tsx .py .sh (gateway-go,
# client-android, andromeda/src, scripts). Markdown TODO notes are out of scope.
#
# Usage:
#   scripts/audit/todo-audit.sh            # diff against baseline
#   scripts/audit/todo-audit.sh --update   # rewrite the baseline
#
# Exit codes: 0 = no new markers; 1 = new markers (listed on stdout);
# 2 = tooling failure.
#
# Baseline edits: do not regenerate to silence a failing diff without operator
# approval — either clear the marker or get the keep approved (same policy as
# deadcode-audit.sh / docs/agent-rules/testing.md).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
baseline="$repo_root/scripts/audit/todo-baseline.txt"
cd "$repo_root"

tracked="$(git ls-files)"

code_files="$(printf '%s\n' "$tracked" | grep -E '\.(go|kt|ts|tsx|py|sh)$' || true)"
if [[ -z "$code_files" ]]; then
    echo "todo-audit: no tracked code files found" >&2
    exit 2
fi

# Comment-anchored scan: a marker only counts when it sits inside a comment, so
# non-debt occurrences (Go's `context.TODO()` stdlib call, regex/string literals
# that merely *mention* the markers) don't pollute the ratchet.
#
#   Go/Kotlin/TS: `// ...` or `/* ...`
#   Python/Shell: `# ...`
#
# Normalize to "path :: line-text" (line number stripped, whitespace collapsed),
# so unrelated line shifts don't churn the baseline.
slash_comments="$(printf '%s\n' "$code_files" |
    grep -E '\.(go|kt|ts|tsx)$' |
    xargs grep -nHE '(//|/\*).*\b(TODO|FIXME|HACK|XXX)\b' 2>/dev/null || true)"
hash_comments="$(printf '%s\n' "$code_files" |
    grep -E '\.(py|sh)$' |
    xargs grep -nHE '#[^\r\n]*\b(TODO|FIXME|HACK|XXX)\b' 2>/dev/null || true)"

current="$({ printf '%s\n' "$slash_comments"; printf '%s\n' "$hash_comments"; } |
    sed -E 's/^([^:]+):[0-9]+:(.*)$/\1 :: \2/' |
    sed -E 's/[[:space:]]+/ /g; s/ $//' |
    sort || true)"

if [[ "${1:-}" == "--update" ]]; then
    {
        echo "# todo-baseline.txt — accepted markers of scripts/audit/todo-audit.sh."
        echo "# Format: <path> :: <line-text>, sorted. Regenerate with --update (operator"
        echo "# approval required per docs/agent-rules/testing.md). Line numbers are"
        echo "# deliberately absent so edits that shift lines don't churn this file."
        echo "#"
        echo "# Keep-policy: markers stay here only if they are (a) deliberate placeholders,"
        echo "# (b) backed by a tracked issue, or (c) pending an operator decision. New"
        echo "# markers must clear an existing one or be approved here."
        echo "$current"
    } > "$baseline"
    echo "todo-audit: baseline updated ($(grep -vc '^#' "$baseline") entries)"
    exit 0
fi

if [[ ! -f "$baseline" ]]; then
    echo "todo-audit: baseline missing; run with --update first" >&2
    exit 2
fi

known="$(grep -v '^#' "$baseline" | sort)"

new_markers="$(comm -13 <(printf '%s\n' "$known") <(printf '%s\n' "$current"))"
resolved="$(comm -23 <(printf '%s\n' "$known") <(printf '%s\n' "$current"))"

count_nonempty_lines() {
    awk 'NF { count++ } END { print count + 0 }'
}

if [[ -n "$resolved" ]]; then
    resolved_count="$(printf '%s\n' "$resolved" | count_nonempty_lines)"
    echo "todo-audit: $resolved_count baseline markers cleared (stale — refresh with --update):"
    echo "$resolved" | sed 's/^/  - /'
fi

if [[ -n "$new_markers" ]]; then
    new_count="$(printf '%s\n' "$new_markers" | count_nonempty_lines)"
    echo "todo-audit: NEW tech-debt markers ($new_count):"
    echo "$new_markers" | sed 's/^/  + /'
    echo "todo-audit: clear the marker (preferred) or baseline it with operator approval."
    exit 1
fi

known_count="$(printf '%s\n' "$known" | count_nonempty_lines)"
echo "todo-audit: clean ($known_count accepted markers, 0 new)"
