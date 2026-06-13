---
description: "Git 워크플로우, PR, 커밋 상세 가이드라인"
globs: [".github/**", "scripts/committer"]
---

# Git Commit & PR Details

## Commit & Pull Request Guidelines

- `/landpr` lives in the global Codex prompts (`~/.codex/prompts/landpr.md`); when landing or merging any PR, always follow that `/landpr` process.
- Create commits with `scripts/committer "<msg>" <file...>`; avoid manual `git add`/`git commit` so staging stays scoped.
- Follow concise, action-oriented commit messages.
- Group related changes; avoid bundling unrelated refactors.
- PR body: follow the canonical skeleton in **PR Body** below. `.github/pull_request_template.md` mirrors it for web-UI / hand-authored PRs.
- Issue submission templates (canonical): `.github/ISSUE_TEMPLATE/`

## PR Body (canonical skeleton)

> 에이전트는 PR 을 `gh pr create --body "…"` 로 만든다 — 이때 **`.github/pull_request_template.md` 는 무시된다**(`--body` 가 덮어씀). 그래서 PR 본문 형식의 단일 진실원은 **이 스켈레톤**이고, `.github/` 파일은 웹 UI/손PR 용 미러일 뿐이다. 형식을 바꾸려면 **둘 다** 고친다.

본문은 **한국어 우선** (코드·식별자·`make check` 등은 예외). 섹션 제목은 영문/국문 어느 쪽이든 무방하나 골격은 고정한다:

```markdown
## Summary          # 무엇을 / 왜 — 문제와 동기 (1~3문단)
## Changes          # 주요 변경점, 파일 경로 포함 불릿 (gateway-go/internal/…:line)
## Verification     # `make check` 통과 명시 + 관련 시 라이브(live-test.sh)·신규 테스트

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

- **필수 3섹션**: Summary · Changes · Verification. `make check` 통과를 Verification 에 명시한다 (빌드가 하드 게이트).
- **선택 섹션**(해당될 때만): `## Before → After`(표로 회귀 방지 증거) · `## 한계 / 리뷰 노트` · `## Follow-up (out of scope)` · `## Cache 영향`(프롬프트 캐시 경로를 건드릴 때 — `.claude/rules/prompt-cache.md` PR 체크리스트 참조).
- **푸터**: 마지막 줄은 항상 `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- PR 제목·커밋 제목은 Conventional Commit 형식 (예: `fix(chat): …`) — CLAUDE.md "Git Commit Format" 참조.

## Git Operations & Safety

- **Stacked PRs: retarget the base to `main` before merging.** Merging a PR whose base is still the parent PR's branch lands the squash commit on that branch — not on main — while GitHub still shows the PR as MERGED. This silently dropped #2119/#2125/#2126 from main on 2026-06-09 (only #2112, whose base was main, actually landed).
- **MERGED state is not proof of landing.** After merging, verify the change is on main: `git merge-base --is-ancestor <mergeCommitSHA> origin/main` (or grep main for the change). Apply this check whenever confirming "merge complete" for stacked or multi-PR work.
- If `git branch -d/-D <branch>` is policy-blocked, delete the local ref directly: `git update-ref -d refs/heads/<branch>`.
- Agents MUST NOT create or push merge commits on `main`. If `main` has advanced, rebase local commits onto the latest `origin/main` before pushing.
- Bulk PR close/reopen safety: if a close action would affect more than 5 PRs, first ask for explicit user confirmation with the exact PR count and target scope/query.
