#!/usr/bin/env bash
# pr.sh — one-command PR check-watching and landing for coding agents.
#
# Encodes the house landing rules (docs/agent-rules/git-pr.md) so each agent
# doesn't re-type them per PR:
#   - checks must be green before merging (watch exits nonzero on any red)
#   - squash merge only
#   - MERGED != LANDED: verify the squash commit is an ancestor of origin/main
#     (the 2026-06-09 stacked-PR incident silently dropped merged work)
#   - delete the remote branch after landing
#
# Usage:
#   scripts/dev/pr.sh watch <pr>   # watch checks; compact red summary; rc!=0 on red
#   scripts/dev/pr.sh land  <pr>   # watch -> squash merge -> landed-verify -> cleanup
set -euo pipefail

cmd="${1:-}"
pr="${2:-}"
if [ -z "$cmd" ] || [ -z "$pr" ]; then
    echo "usage: $(basename "$0") {watch|land} <pr-number>" >&2
    exit 2
fi

watch_checks() {
    # gh pr checks --watch exits 0 only when every required check passes.
    if gh pr checks "$pr" --watch --interval 30 >/dev/null 2>&1; then
        echo "PR #$pr: checks green"
        return 0
    fi
    echo "PR #$pr: checks NOT green —" >&2
    gh pr checks "$pr" 2>/dev/null \
        | awk -F'\t' '$2 != "pass" && $2 != "skipping" { print "  " $1 ": " $2 }' >&2
    return 1
}

case "$cmd" in
watch)
    watch_checks
    ;;
land)
    watch_checks
    # Tolerate an operator racing us to the merge button.
    if [ "$(gh pr view "$pr" --json state -q .state)" != "MERGED" ]; then
        gh pr merge "$pr" --squash
    fi
    sha="$(gh pr view "$pr" --json mergeCommit -q .mergeCommit.oid)"
    git fetch origin main --quiet
    if ! git merge-base --is-ancestor "$sha" origin/main; then
        echo "PR #$pr shows MERGED but $sha is NOT on origin/main — stacked base? Investigate before trusting this merge." >&2
        exit 1
    fi
    branch="$(gh pr view "$pr" --json headRefName -q .headRefName)"
    git push origin --delete "$branch" >/dev/null 2>&1 || true
    echo "PR #$pr landed: $sha is on origin/main (branch $branch cleaned up)"
    gh pr view "$pr" --json url -q .url
    ;;
*)
    echo "usage: $(basename "$0") {watch|land} <pr-number>" >&2
    exit 2
    ;;
esac
