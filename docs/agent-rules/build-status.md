---
description: "빌드 상태 확인: 릴리즈 버전, 커밋 델타, 브랜치 비교"
globs: [".github/workflows/**", "scripts/build-status"]
---

# Build Status: Release Version & Commit Comparison

## `scripts/build-status` (git 기반 — 기본 경로)

| Command | Output |
|---|---|
| `scripts/build-status` | Full report (main + current branch) |
| `scripts/build-status main` | main: 릴리즈 버전, 릴리즈 이후 커밋 목록 |
| `scripts/build-status current` | 현재 브랜치 vs main 델타 (+ahead/-behind, 내 커밋 목록) |

출력 형태: `release: v4.7.0 (deneb-v4.7.0)` / `commits since release: N` / 브랜치 섹션은
`vs main: +3 ahead, -5 behind` + 미머지 커밋 목록.

git 을 못 쓰는 환경에서만 GitHub MCP 로 대체한다: `get_latest_release` → 태그,
`list_commits(sha=main)` 에서 `chore: release vX.Y.Z` 커밋까지 스캔이 릴리즈 델타,
`pull_request_read` 가 브랜치 비교(commits/additions/behind 여부).

## Version System

- **Tag format**: `deneb-vX.Y.Z` (release-please managed)
- **Version files**: `.release-please-manifest.json`, `package.json`
- **Build injection**: Makefile 이 최신 `deneb-v*` 태그를 Go ldflags `-X main.Version` 으로 주입
- **Changelog**: `CHANGELOG.md` (conventional commit 타입별 자동 생성 — 직접 편집 금지)
