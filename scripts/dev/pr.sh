#!/usr/bin/env bash
# pr.sh — one-command PR check-watching and landing for coding agents.
#
# Encodes the house landing rules (docs/agent-rules/git-pr.md) so each agent
# doesn't re-type them per PR:
#   - checks must be green before merging (watch exits nonzero on any red)
#   - squash merge only
#   - MERGED != LANDED: verify the squash commit is an ancestor of origin/main
#     (the 2026-06-09 stacked-PR incident silently dropped merged work)
#   - interaction guard: refuse to land only when main's NEWER commits touch a
#     file this PR also changes — the green CI then predates the merged tree
#     (the 2026-07-08 stale-worktree incident). Non-overlapping staleness (a
#     trivial edit while main moved elsewhere) lands as-is: no rebase, no CI
#     re-run. main has no branch protection, so keep this surgical, not blanket.
#   - delete the remote branch after landing
#   - attach the CodeGraph blast-radius brief to the PR body (review evidence)
#
# Usage:
#   scripts/dev/pr.sh watch  <pr>   # watch checks; compact red summary; rc!=0 on red
#   scripts/dev/pr.sh land   <pr>   # watch -> squash merge -> landed-verify -> cleanup
#   scripts/dev/pr.sh attach <pr>   # (re)attach the blast-radius brief only
set -euo pipefail

cmd="${1:-}"
pr="${2:-}"
if [ -z "$cmd" ] || [ -z "$pr" ]; then
    echo "usage: $(basename "$0") {watch|land|attach} <pr-number>" >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# attach_impact_brief posts the deterministic CodeGraph blast radius of this
# branch into the PR body, under replaceable markers.
#
# Why HERE and not in the agent's prompt: an agent asked to include its own
# blast radius is an agent that can decide not to. This runs on the landing
# path every agent already has to use, and pr.sh is a forbidden self-edit
# surface for the L4 self-coding lane — so the accountability record is one the
# lane cannot switch off for itself.
#
# Never fatal. A missing index, missing binary, or a gh failure degrades the
# review evidence; it must not block a green PR from landing.
attach_impact_brief() {
    local pr="$1"
    command -v gh >/dev/null 2>&1 || return 0
    command -v python3 >/dev/null 2>&1 || return 0
    [ -f "$SCRIPT_DIR/impact_brief.py" ] || return 0

    # The brief is computed from the working tree (codegraph indexes files on
    # disk), so it only describes THIS PR when the checkout sits at its head.
    # Landing someone else's PR from an unrelated tree must not attach a brief
    # about the wrong code.
    local head_oid local_head root
    head_oid="$(gh pr view "$pr" --json headRefOid -q .headRefOid 2>/dev/null || true)"
    local_head="$(git rev-parse --quiet --verify HEAD 2>/dev/null || true)"
    if [ -z "$head_oid" ] || [ "$head_oid" != "$local_head" ]; then
        echo "PR #$pr: impact brief skipped (checkout is not at the PR head)" >&2
        return 0
    fi
    root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
    [ -n "$root" ] || return 0
    git fetch origin main --quiet 2>/dev/null || true

    local brief body
    brief="$(python3 "$SCRIPT_DIR/impact_brief.py" --repo "$root" --range "origin/main...HEAD" 2>/dev/null || true)"
    if [ -z "$brief" ]; then
        echo "PR #$pr: impact brief unavailable (non-fatal)" >&2
        return 0
    fi
    body="$(gh pr view "$pr" --json body -q .body 2>/dev/null || true)"
    local merged
    merged="$(BRIEF="$brief" BODY="$body" python3 -c '
import os, sys
sys.path.insert(0, os.environ["SCRIPT_DIR"])
from impact_brief import splice_into_body
sys.stdout.write(splice_into_body(os.environ["BODY"], os.environ["BRIEF"]))
' 2>/dev/null || true)"
    if [ -z "$merged" ]; then
        echo "PR #$pr: impact brief splice failed (non-fatal)" >&2
        return 0
    fi
    if printf '%s' "$merged" | gh pr edit "$pr" --body-file - >/dev/null 2>&1; then
        echo "PR #$pr: impact brief attached"
    else
        echo "PR #$pr: impact brief attach failed (non-fatal)" >&2
    fi
}
export SCRIPT_DIR

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
attach)
    attach_impact_brief "$pr"
    ;;
watch)
    # Deliberately NOT attaching here: watch is a cheap poll agents call
    # repeatedly, and its no-extra-API-calls contract is pinned by
    # test_pr_shell. land attaches before it starts waiting, which puts the
    # brief on the PR while CI is still running.
    watch_checks
    ;;
land)
    attach_impact_brief "$pr"
    watch_checks
    # Multi-agent guard (2026-07-06 incident, PR #3219): the branch name is a
    # shared surface — a parallel session can replace the branch's commit
    # between push and merge, and a SINGLE-commit PR squash then lands that
    # commit under this PR's number (GitHub uses the commit message, not the
    # PR title, as the squash title for single-commit PRs). When this clone
    # has a local ref of the same branch, require it to match the PR head.
    branch="$(gh pr view "$pr" --json headRefName -q .headRefName)"
    head_oid="$(gh pr view "$pr" --json headRefOid -q .headRefOid)"
    local_oid="$(git rev-parse --quiet --verify "refs/heads/$branch" 2>/dev/null || true)"
    if [ -n "$local_oid" ] && [ "$local_oid" != "$head_oid" ]; then
        echo "PR #$pr head is $head_oid but local branch $branch is $local_oid —" >&2
        echo "the remote branch changed since your push (parallel session?). Refusing to land." >&2
        exit 1
    fi
    # Merge — unless an operator or auto-merge already landed this PR while we
    # watched checks (then the guard below is moot; origin/main already carries
    # this PR's squash, which a naive behind-count would misread as "stale").
    if [ "$(gh pr view "$pr" --json state -q .state)" != "MERGED" ]; then
        # Interaction guard (2026-07-08 stale-worktree incident, calibrated): a
        # behind branch is only unsafe to land when main's NEWER commits touch a
        # file this PR also changes — then the green CI, run on a base without
        # those changes, no longer describes the merged tree. Non-overlapping
        # staleness (a trivial edit while main moved elsewhere) is safe to land
        # as-is: no rebase, no CI re-run. main has no branch protection, so this
        # is the only up-to-date check — keep it surgical, not blanket.
        # Override with PR_LAND_ALLOW_STALE=1.
        git fetch origin main "refs/pull/$pr/head" --quiet 2>/dev/null || git fetch origin main --quiet
        mb="$(git merge-base "$head_oid" origin/main 2>/dev/null || true)"
        if [ -n "$mb" ] && [ "${PR_LAND_ALLOW_STALE:-0}" != "1" ]; then
            overlap="$(comm -12 \
                <(git diff --name-only "$mb" "$head_oid" | sort -u) \
                <(git diff --name-only "$mb" origin/main | sort -u) || true)"
            if [ -n "$overlap" ]; then
                echo "PR #$pr shares files with newer origin/main commits — its green CI predates them:" >&2
                while IFS= read -r f; do echo "    $f" >&2; done <<<"$overlap"
                echo "Rebase onto origin/main + re-run 'make check' before landing (or PR_LAND_ALLOW_STALE=1)." >&2
                exit 1
            fi
        fi
        gh pr merge "$pr" --squash
    fi
    sha="$(gh pr view "$pr" --json mergeCommit -q .mergeCommit.oid)"
    git fetch origin main --quiet
    if ! git merge-base --is-ancestor "$sha" origin/main; then
        echo "PR #$pr shows MERGED but $sha is NOT on origin/main — stacked base? Investigate before trusting this merge." >&2
        exit 1
    fi
    git push origin --delete "$branch" >/dev/null 2>&1 || true
    echo "PR #$pr landed: $sha is on origin/main (branch $branch cleaned up)"
    gh pr view "$pr" --json url -q .url
    ;;
*)
    echo "usage: $(basename "$0") {watch|land|attach} <pr-number>" >&2
    exit 2
    ;;
esac
