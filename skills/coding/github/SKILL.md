---
name: github
version: "1.0.0"
category: coding
description: "GitHub operations via `gh` CLI: PRs (list, diff, review, comment, merge), issues (CRUD, labels, milestones), CI (status, logs, dispatch), search (code, PRs, issues), releases, repos, and API/GraphQL queries. Use when: (1) viewing PR diffs or changed files, (2) submitting PR reviews, (3) commenting on PRs/issues with safe heredoc, (4) managing labels, (5) searching code/PRs/issues, (6) checking or re-running CI, (7) creating releases, (8) dispatching workflows, (9) querying GitHub API or GraphQL. NOT for: local git operations (use git directly), non-GitHub hosts, complex code review (use coding-agent), or when gh auth is not configured."
metadata:
  {
    "deneb":
      {
        "emoji": "🐙",
        "requires": { "bins": ["gh"] },
        "tags": ["git", "PR", "issues", "CI", "review"],
        "related_skills": ["coding-agent"],
        "install":
          [
            {
              "id": "brew",
              "kind": "brew",
              "formula": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (brew)",
            },
            {
              "id": "apt",
              "kind": "apt",
              "package": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (apt)",
            },
          ],
      },
  }
---

# GitHub Skill

Use the `gh` CLI to interact with GitHub repositories, issues, PRs, CI, releases, and more.

## When to Use

✅ **USE this skill when:**

- Checking PR status, diffs, changed files, or merge readiness
- Submitting PR reviews (approve, request changes, comment)
- Commenting on PRs or issues (especially multiline with heredoc)
- Managing labels on PRs and issues
- Searching code, PRs, or issues across repositories
- Viewing CI/workflow run status and logs
- Dispatching or re-running workflows
- Creating, closing, or editing issues
- Creating or merging pull requests
- Creating releases or downloading release assets
- Querying GitHub API (REST or GraphQL)
- Cloning, forking, or viewing repository info

## When NOT to Use

❌ **DON'T use this skill when:**

- Local git operations (commit, push, pull, branch, rebase) → use `git` directly
- Non-GitHub repos (GitLab, Bitbucket, self-hosted) → different CLIs
- Reviewing actual code changes in depth → use `coding-agent` skill
- Complex multi-file code review requiring agent reasoning → use `coding-agent`
- Bulk operations across many repos → script with `gh api` + loops

## Setup

```bash
# Authenticate (one-time)
gh auth login

# Verify
gh auth status
```

## Pull Requests

### List and View

```bash
# List open PRs
gh pr list --repo owner/repo

# List with specific state
gh pr list --repo owner/repo --state merged --limit 20

# View PR details
gh pr view 55 --repo owner/repo

# View with structured fields
gh pr view 55 --repo owner/repo --json title,body,author,state,reviewDecision,mergeable
```

### Diff and Changed Files

```bash
# View full diff
gh pr diff 55 --repo owner/repo

# List changed file names only
gh pr diff 55 --repo owner/repo --name-only

# Changed files with additions/deletions count
gh pr view 55 --repo owner/repo --json files \
  --jq '.files[] | "\(.additions)+/\(.deletions)- \(.path)"'

# Just file paths
gh pr view 55 --repo owner/repo --json files --jq '.files[].path'
```

### Create and Merge

```bash
# Create PR (simple)
gh pr create --title "feat: add feature" --body "Description" --repo owner/repo

# Create PR with heredoc body
gh pr create --title "fix: resolve crash" --repo owner/repo -F - <<'EOF'
## Summary

Fixed the null pointer crash in the auth module.

## Test Plan

- [x] Unit tests pass
- [x] Manual verification on staging
EOF

# Merge PR (squash)
gh pr merge 55 --squash --repo owner/repo

# Merge PR (rebase)
gh pr merge 55 --rebase --repo owner/repo

# Merge with auto-merge (waits for CI)
gh pr merge 55 --squash --auto --repo owner/repo
```

### Review Submission

```bash
# Approve
gh pr review 55 --approve --repo owner/repo

# Approve with comment
gh pr review 55 --approve --body "LGTM, nice work!" --repo owner/repo

# Request changes
gh pr review 55 --request-changes --repo owner/repo -F - <<'EOF'
A few issues to address:

1. Missing error handling in `processInput()`
2. Test coverage for the new edge case
EOF

# Leave a review comment (no approval/rejection)
gh pr review 55 --comment --body "Looks good overall, one question below." --repo owner/repo
```

### PR Comments

````bash
# Simple comment
gh pr comment 55 --body "Looks good to merge." --repo owner/repo

# Multiline comment (heredoc — recommended for anything with special chars)
gh pr comment 55 --repo owner/repo -F - <<'EOF'
## Review Notes

- `parseConfig()` needs null check at line 42
- Consider using `Map` instead of plain object for better perf

```ts
// suggested fix
if (!config) return defaultConfig;
````

EOF

# Reply to a review thread via API

gh api repos/owner/repo/pulls/55/comments \
 --jq '.[] | "\(.id): \(.body[:80])"'

gh api repos/owner/repo/pulls/comments/12345/replies \
 -f body="Fixed in the latest commit."

````

### Branch Operations

```bash
# Checkout a PR locally
gh pr checkout 55

# Checkout from a different repo
gh pr checkout 55 --repo owner/repo

# Checkout detached (don't create local branch)
gh pr checkout 55 --detach
````

## Issues

### List, Create, and Close

```bash
# List open issues
gh issue list --repo owner/repo --state open

# List with label filter
gh issue list --repo owner/repo --label "bug" --limit 30

# Create issue
gh issue create --title "Bug: something broken" --body "Details..." --repo owner/repo

# Create with heredoc body
gh issue create --title "Feature: add dark mode" --repo owner/repo -F - <<'EOF'
## Description

Add dark mode support to the settings panel.

## Acceptance Criteria

- [ ] Toggle in settings
- [ ] Persists across sessions
- [ ] Respects system preference
EOF

# Close issue
gh issue close 42 --repo owner/repo

# Reopen issue
gh issue reopen 42 --repo owner/repo
```

### Edit Issues

```bash
# Add labels
gh issue edit 42 --add-label "bug" --repo owner/repo

# Remove labels
gh issue edit 42 --remove-label "triage" --repo owner/repo

# Set assignee
gh issue edit 42 --add-assignee "@me" --repo owner/repo

# Set milestone
gh issue edit 42 --milestone "v2.0" --repo owner/repo

# Comment on issue (heredoc)
gh issue comment 42 --repo owner/repo -F - <<'EOF'
Investigated this — root cause is in `src/auth/token.ts:85`.
The refresh token expiry check uses `<` instead of `<=`.

Fix incoming.
EOF
```

## Labels

```bash
# List all labels
gh label list --repo owner/repo

# Create a label
gh label create "needs-review" --color "0e8a16" --description "Awaiting code review" --repo owner/repo

# Add label to PR
gh pr edit 55 --add-label "needs-review" --repo owner/repo

# Add/remove labels on issue
gh issue edit 42 --add-label "bug" --remove-label "triage" --repo owner/repo

# Bulk: list labels as JSON
gh label list --repo owner/repo --json name,color,description \
  --jq '.[] | "\(.name) (\(.color)): \(.description)"'
```

## Search

### Code Search

```bash
# Search for a function
gh search code "functionName" --repo owner/repo

# Search with file type filter
gh search code "import { Router }" --repo owner/repo --filename "*.ts"

# Search with path filter
gh search code "TODO" --repo owner/repo --filename "src/*"
```

### PR and Issue Search

```bash
# Search PRs by keyword
gh search prs --repo owner/repo --match title,body --limit 50 -- "auto-update"

# Search with filters
gh search prs --repo owner/repo --author "@me" --state merged --merged ">2026-01-01"

# Search issues with structured output
gh search issues --repo owner/repo --match title,body --limit 50 \
  --json number,title,state,url,updatedAt -- "crash" \
  --jq '.[] | "\(.number) | \(.state) | \(.title) | \(.url)"'

# Search open bugs
gh search issues --repo owner/repo --state open --label "bug" --limit 30

# Add --match comments for deeper search
gh search issues --repo owner/repo --match title,body,comments --limit 50 -- "regression"
```

## CI / Workflow Runs

### View Status and Logs

```bash
# List recent runs
gh run list --repo owner/repo --limit 10

# List runs for a specific workflow
gh run list --repo owner/repo --workflow "ci.yml" --limit 5

# View specific run
gh run view <run-id> --repo owner/repo

# View failed step logs only
gh run view <run-id> --repo owner/repo --log-failed

# Re-run failed jobs
gh run rerun <run-id> --failed --repo owner/repo
```

### Workflow Dispatch

```bash
# List available workflows
gh workflow list --repo owner/repo

# Trigger a workflow
gh workflow run "ci.yml" --repo owner/repo

# Trigger with ref and inputs
gh workflow run "deploy.yml" --repo owner/repo --ref main -f environment=staging -f version=1.2.3

# Watch a run until completion
gh run watch <run-id> --repo owner/repo
```

### Check PR CI Status

```bash
# Quick CI status
gh pr checks 55 --repo owner/repo

# CI status as JSON
gh pr checks 55 --repo owner/repo --json name,state,conclusion \
  --jq '.[] | "\(.name): \(.conclusion // .state)"'
```

## Releases

```bash
# List releases
gh release list --repo owner/repo --limit 10

# View specific release
gh release view v1.2.3 --repo owner/repo

# Create release with auto-generated notes
gh release create v1.2.3 --generate-notes --repo owner/repo

# Create release with custom notes
gh release create v1.2.3 --title "v1.2.3" --repo owner/repo -F - <<'EOF'
## Changes

- Added dark mode support
- Fixed auth token refresh bug
- Improved startup performance by 30%
EOF

# Create pre-release
gh release create v1.3.0-beta.1 --prerelease --generate-notes --repo owner/repo

# Download release assets
gh release download v1.2.3 --repo owner/repo --dir /tmp/release

# Get latest release info
gh release view --repo owner/repo --json tagName,publishedAt,body
```

## Repository

```bash
# View repo info
gh repo view owner/repo

# View as JSON
gh repo view owner/repo --json defaultBranchRef,description,url,stargazerCount

# Clone repo
gh repo clone owner/repo /tmp/review-dir

# Fork repo
gh repo fork owner/repo --clone
```

## API and GraphQL

### REST API

```bash
# Get PR with specific fields
gh api repos/owner/repo/pulls/55 --jq '.title, .state, .user.login'

# List all labels
gh api repos/owner/repo/labels --jq '.[].name'

# Get repo stats
gh api repos/owner/repo --jq '{stars: .stargazers_count, forks: .forks_count}'

# Paginated results (auto-follows next pages)
gh api repos/owner/repo/pulls --paginate --jq '.[].number'

# POST request
gh api repos/owner/repo/issues/42/labels -f labels[]="bug"

# Check rate limit
gh api rate_limit --jq '.rate | "\(.remaining)/\(.limit) (resets \(.reset | strftime("%H:%M:%S")))"'
```

### GraphQL

```bash
# Get PR review decision and timeline
gh api graphql -f query='
  query {
    repository(owner: "owner", name: "repo") {
      pullRequest(number: 55) {
        reviewDecision
        reviews(last: 5) {
          nodes { state author { login } }
        }
      }
    }
  }
'

# Search with GraphQL
gh api graphql -f query='
  query($q: String!) {
    search(query: $q, type: ISSUE, first: 10) {
      nodes { ... on Issue { number title state } }
    }
  }
' -f q="repo:owner/repo is:open label:bug"
```

## JSON Output

Most commands support `--json` for structured output with `--jq` filtering:

```bash
gh issue list --repo owner/repo --json number,title \
  --jq '.[] | "\(.number): \(.title)"'

gh pr list --json number,title,state,mergeable \
  --jq '.[] | select(.mergeable == "MERGEABLE")'
```

### Commonly Useful `--json` Fields

| Resource | Useful fields                                                                                                                       |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| PR       | `number`, `title`, `state`, `author`, `body`, `mergeable`, `reviewDecision`, `files`, `additions`, `deletions`, `statusCheckRollup` |
| Issue    | `number`, `title`, `state`, `labels`, `assignees`, `milestone`, `createdAt`, `updatedAt`                                            |
| Run      | `databaseId`, `name`, `status`, `conclusion`, `headBranch`, `event`                                                                 |

## Patterns and Best Practices

### Safe Heredoc for Multiline Text

Always use heredoc (`-F - <<'EOF'`) for comment/body text containing backticks, special characters, or newlines. Never use `-b "..."` with complex content.

````bash
# Correct: single-quoted EOF prevents shell expansion
gh pr comment 55 --repo owner/repo -F - <<'EOF'
## Review

Code looks clean. One suggestion:

```ts
const result = await fetchData();
````

Tested locally — all green.
EOF

# Wrong: shell will break on backticks and special chars

# gh pr comment 55 -b "Use `fetchData()` instead..."

````

### Batch Operations

```bash
# Close all PRs with a label
gh pr list --repo owner/repo --label "stale" --json number --jq '.[].number' | \
  xargs -I{} gh pr close {} --repo owner/repo

# Add label to all open issues matching a search
gh search issues --repo owner/repo --state open -- "legacy" \
  --json number --jq '.[].number' | \
  xargs -I{} gh issue edit {} --add-label "tech-debt" --repo owner/repo
````

> **Safety:** If a batch operation affects more than 5 items, list the items first and confirm before executing.

### Error Handling

```bash
# Auth issues
gh auth status          # Check current auth
gh auth refresh         # Refresh expired token

# Rate limit check
gh api rate_limit --jq '.rate | "\(.remaining)/\(.limit)"'

# Use cache for repeated queries
gh api repos/owner/repo --cache 1h
```

| Error          | Cause                                    | Fix                            |
| -------------- | ---------------------------------------- | ------------------------------ |
| `HTTP 404`     | Repo not found or no access              | Check repo name and auth scope |
| `HTTP 403`     | Rate limited or insufficient permissions | Wait or `gh auth refresh`      |
| `HTTP 422`     | Invalid request (bad field, duplicate)   | Check request body/params      |
| `GraphQL: ...` | Query syntax error                       | Validate query structure       |

## Templates

### PR Review Summary

```bash
PR=55 REPO=owner/repo
echo "## PR #$PR Summary"
gh pr view $PR --repo $REPO --json title,body,author,additions,deletions,changedFiles \
  --jq '"**\(.title)** by @\(.author.login)\n\n\(.body)\n\n+\(.additions) -\(.deletions) across \(.changedFiles) files"'
gh pr checks $PR --repo $REPO
```

### Issue Triage

```bash
gh issue list --repo owner/repo --state open --json number,title,labels,createdAt \
  --jq '.[] | "[\(.number)] \(.title) - \([.labels[].name] | join(", ")) (\(.createdAt[:10]))"'
```

### CI Failure Diagnosis

```bash
# Get the latest failed run and its logs
RUN_ID=$(gh run list --repo owner/repo --status failure --limit 1 --json databaseId --jq '.[0].databaseId')
echo "Failed run: $RUN_ID"
gh run view $RUN_ID --repo owner/repo --log-failed 2>&1 | tail -50
```

## Notes

- Always specify `--repo owner/repo` when not in a git directory
- Use URLs directly: `gh pr view https://github.com/owner/repo/pull/55`
- Set `GH_REPO=owner/repo` env var to avoid repeating `--repo` flag
- Use `--paginate` for API endpoints that return paginated results
- Use `--cache <duration>` for repeated API queries to avoid rate limits
- For multiline text, always use `-F - <<'EOF'` heredoc (never `-b "..."` with special chars)
- Rate limits apply; check with `gh api rate_limit` if hitting errors
