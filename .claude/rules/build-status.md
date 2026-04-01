---
description: "CI 빌드 상태 확인 가이드 (GitHub Actions)"
globs: [".github/workflows/**", "scripts/build-status"]
---

# Checking CI Build Status

## Using `scripts/build-status` (when `gh` CLI is available)

| Command | Purpose |
|---|---|
| `scripts/build-status main` | Is main green? |
| `scripts/build-status current` | CI status of current branch/PR |
| `scripts/build-status pr <N>` | CI status of specific PR |

Exit codes: `0`=pass, `1`=fail, `2`=pending, `3`=no data.

First line of output is machine-parseable: `PASS|FAIL|PENDING <ref> <sha> (<age>)`.

## Using MCP Tools (when `gh` CLI is unavailable)

### Check main branch status

1. Find the latest merged PR to main:
   ```
   mcp__github__search_pull_requests(
     query="is:merged base:main sort:updated-desc",
     owner="choiceoh", repo="deneb", perPage=1
   )
   ```
2. Get check runs for that PR:
   ```
   mcp__github__pull_request_read(
     method="get_check_runs",
     owner="choiceoh", repo="deneb", pullNumber=<PR>
   )
   ```
3. Or get combined status:
   ```
   mcp__github__pull_request_read(
     method="get_status",
     owner="choiceoh", repo="deneb", pullNumber=<PR>
   )
   ```

### Check current branch / PR status

1. Find the PR for the current branch:
   ```
   mcp__github__list_pull_requests(
     owner="choiceoh", repo="deneb",
     head="choiceoh:<branch-name>", state="open"
   )
   ```
   Note: the `head` filter requires the `owner:branch` format (e.g. `choiceoh:feat/my-feature`).

2. Get check runs:
   ```
   mcp__github__pull_request_read(
     method="get_check_runs",
     owner="choiceoh", repo="deneb", pullNumber=<PR>
   )
   ```

## Interpreting CI Results

Main CI pipeline jobs (from `ci.yml`):

| Job | What it checks |
|---|---|
| `docs-scope` | Scoping job — detects docs-only changes to skip heavy jobs. Not a real gate. |
| `clippy` | Rust lint (deny warnings) for core-rs and cli-rs |
| `core-rs` | Rust core build + test (release mode) |
| `cli-rs` | Rust CLI build + test |
| `go-pure` | Go build + vet + format + lint (no FFI) |
| `go-ffi` | Go build + test with Rust FFI, race detector, coverage, integration, fuzz |
| `integration` | End-to-end gateway tests (CLI flags, health, RPC) |
| `secrets` | Pre-commit secret detection + zizmor workflow audit |

- If `docs_only=true`, jobs from `clippy` through `integration` are skipped — this is normal for docs-only PRs.
- A green main means all 7 substantive jobs (clippy through secrets) passed.
- Other workflows (`generate-check`, `proto-check`, `docs`, `workflow-sanity`) run independently and may appear as additional check runs.
