---
name: review-closeout
version: "1.0.0"
category: coding
description: "Close out PR review comments and ship safely. Use when: PR review comments arrive, a second-model/code review must be triaged, or a PR needs final proof before merge. NOT for: simple GitHub lookup (use github), broad redesigns, or unrelated cleanup."
metadata:
  {
    "deneb":
      {
        "emoji": "🔎",
        "requires": { "bins": ["git", "gh"] },
        "tags": ["PR", "review", "closeout", "scope", "validation", "transcript"],
        "related_skills": ["github", "taskflow", "remote-validation", "session-logs"],
        "install":
          [
            {
              "id": "brew-gh",
              "kind": "brew",
              "formula": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (brew)",
            },
            {
              "id": "apt-gh",
              "kind": "apt",
              "package": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (apt)",
            },
          ],
      },
  }
---

# Review Closeout

Use this when a PR receives review comments or when a change needs final review
discipline before merge.

## Procedure

1. Freeze the scope: original user request, PR number, base branch, changed
   files, and the behavior the PR claims to change.
2. Fetch review material with `gh`: PR summary, review threads, issue comments,
   checks, and changed files.
3. Classify each finding:
   - **blocker**: introduced by this PR, inside the same ownership boundary,
     and fixable without changing the PR contract.
   - **follow-up**: real, but adjacent or broader than this PR.
   - **stop**: requires a new protocol/config/storage/API/release decision.
4. Patch only blockers. Do not expand the PR just to satisfy speculative review
   comments.
5. Run focused validation after every patch. If review-triggered fixes touch a
   shared path, broaden tests to the smallest meaningful owner boundary.
6. Reply to each actionable review thread with the fix, skipped reason, or
   follow-up boundary. Use heredocs for multi-line comments.
7. Before merge, prove the actual landing target: PR checks, branch diff, and
   after-merge `origin/main` commit when the user requested merge.

## Scope Governor

Pause and report instead of editing when any of these happen:

- The fix needs a new architecture, protocol, config surface, migration, or
  release process.
- The patch would more than double the original touched files or non-test LOC.
- Two review-fix cycles have not converged.
- The correct answer is "define the canonical contract first".
- Fixing the comment would make the PR no longer describe the same change.

Critical exceptions are concrete data loss, crash, broken install/upgrade,
release blocker, or active security exposure. Say explicitly when one applies.

## Evidence

- Preserve review evidence in the PR body or comments: tests run, checks,
  commit SHA, and any remaining follow-up.
- When a future agent needs to audit why a change happened, include a compact
  session/provenance note: session key if known, changed files, tests, and PR
  URL. Never paste raw local logs or secrets into GitHub.
- If a review exposes a reusable agent mistake but you cannot safely fix it now,
  record it as `skill_lifecycle` `self_correction` with title, evidence,
  targetFiles, proposedChange, and risk.

## Verification

Minimum closeout proof:

```bash
git status --short
gh pr view <pr> --json number,title,state,reviewDecision,mergeable,statusCheckRollup
gh pr diff <pr> --name-only
```

For a merge request, continue through the merge and verify the resulting commit:

```bash
gh pr merge <pr> --squash --delete-branch
git fetch --prune
git rev-parse origin/main
git log -1 --oneline origin/main
```
