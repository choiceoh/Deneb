---
description: "빌드 상태 확인: 릴리즈 버전, 커밋 델타, 브랜치 비교"
globs: [".github/workflows/**", "scripts/build-status"]
---

# Build Status: Release Version & Commit Comparison

## Using `scripts/build-status` (local, git-based)

| Command | Output |
|---|---|
| `scripts/build-status` | Full report (main + current branch) |
| `scripts/build-status main` | Main: release version, commits since release |
| `scripts/build-status current` | Current branch vs main delta |

Example output:
```
=== main ===
  release:  v4.7.0 (deneb-v4.7.0)
  tag SHA:  abc1234
  HEAD SHA: def5678
  commits since release: 12

  commits since v4.7.0:
    abc1234 feat(exec): enhance exec tool
    def5678 fix(chat): prevent tool call leak
    ...

=== my-branch ===
  HEAD SHA:   fed8765
  base:       v4.7.0 (main)
  vs main:    +3 ahead, -5 behind

  your commits (not in main):
    fed8765 feat(foo): add bar
    ...
```

## Using MCP Tools (when git is unavailable)

### Main branch status

1. Get latest release version and tag:
   ```
   mcp__github__get_latest_release(owner="choiceoh", repo="deneb")
   ```
   → Returns tag name (`deneb-vX.Y.Z`), published date, release notes.

2. Get recent main commits (to see what's landed since release):
   ```
   mcp__github__list_commits(owner="choiceoh", repo="deneb", sha="main", perPage=15)
   ```

3. Compare release tag to main HEAD:
   ```
   mcp__github__list_commits(owner="choiceoh", repo="deneb", sha="main", perPage=30)
   ```
   Scan until you find the release commit (title matches `chore: release vX.Y.Z`).

### Current branch vs main

1. Get current branch commits:
   ```
   mcp__github__list_commits(owner="choiceoh", repo="deneb", sha="<branch-name>", perPage=15)
   ```

2. Compare with main to see delta — look at the latest common ancestor.

3. For a PR-based comparison:
   ```
   mcp__github__pull_request_read(method="get", owner="choiceoh", repo="deneb", pullNumber=<N>)
   ```
   → Shows `commits` count, `additions`, `deletions`, `changed_files`, and whether the branch is behind base.

## Version System

- **Tag format**: `deneb-vX.Y.Z` (release-please managed)
- **Version files**: `.release-please-manifest.json`, `package.json`
- **Build injection**: Makefile extracts latest `deneb-v*` tag → Go ldflags `-X main.Version`
- **Changelog**: `CHANGELOG.md` (grouped by conventional commit type)
