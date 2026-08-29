---
description: "Git 워크플로우, PR, 커밋 상세 가이드라인"
globs: [".github/**", "scripts/committer"]
---

# Git Commit & PR Details

## Commit & Pull Request Guidelines

- `/landpr` lives in the global Codex prompts (`~/.codex/prompts/landpr.md`); when landing or merging any PR, always follow that `/landpr` process.
- **Land PRs with `scripts/dev/pr.sh land <pr>`** (or `watch <pr>` to just gate on checks): it watches checks to green, squash-merges, verifies the squash commit actually reached `origin/main` (MERGED ≠ LANDED — see below), and deletes the remote branch. One command instead of five, and the landing invariants can't be skipped.
- **The blast-radius brief is attached automatically.** `watch`/`land` (and `attach <pr>` on its own) run `scripts/dev/impact_brief.py` and splice a CodeGraph section into the PR body — the symbols this diff edits, which production symbols outside the diff they reach, and which have no test coverage. Deterministic graph queries, no LLM. It replaces its own previous block rather than stacking, and skips silently when the checkout isn't at the PR head or the index is missing. Don't hand-maintain that section; edit the code or the script.
- Create commits with `scripts/committer "<msg>" <file...>`; avoid manual `git add`/`git commit` so staging stays scoped.
- **ZCode/Codex helpers** (when pre-commit hooks or multi-agent push collisions slow you down):
  - `scripts/dev/zcode-commit.sh "<msg>" <files...>` — runs local shellcheck/golangci-lint first, then tries `committer`; on Docker/OrbStack pre-commit failure, falls back to `--no-verify` (safe — local validation already passed).
  - `scripts/dev/zcode-push.sh [branch]` — auto `fetch + rebase + push` in one step; handles the non-fast-forward rejections that happen when other agents push concurrently.
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
- **선택 섹션**(해당될 때만): `## Before → After`(표로 회귀 방지 증거) · `## 한계 / 리뷰 노트` · `## Follow-up (out of scope)` · `## Cache 영향`(프롬프트 캐시 경로를 건드릴 때 — `docs/agent-rules/prompt-cache.md` PR 체크리스트 참조).
- **푸터**: 에이전트 생성 PR 은 마지막 줄에 항상 `🤖 Generated with [Claude Code](https://claude.com/claude-code)`. (웹 UI/손PR 용 `.github/pull_request_template.md` 미러에는 이 푸터가 의도적으로 없다 — 사람 PR 에는 해당 없음.)
- PR 제목·커밋 제목은 Conventional Commit 형식 (예: `fix(chat): …`) — CLAUDE.md "Git Commit Format" 참조.

## Git Operations & Safety

- **Stacked PRs: retarget the base to `main` before merging.** Merging a PR whose base is still the parent PR's branch lands the squash commit on that branch — not on main — while GitHub still shows the PR as MERGED. This silently dropped #2119/#2125/#2126 from main on 2026-06-09 (only #2112, whose base was main, actually landed).
- **MERGED state is not proof of landing.** After merging, verify the change is on main: `git merge-base --is-ancestor <mergeCommitSHA> origin/main` (or grep main for the change). Apply this check whenever confirming "merge complete" for stacked or multi-PR work.
- If `git branch -d/-D <branch>` is policy-blocked, delete the local ref directly: `git update-ref -d refs/heads/<branch>`.
- Agents MUST NOT create or push merge commits on `main`. If `main` has advanced, rebase local commits onto the latest `origin/main` before pushing.
- Bulk PR close/reopen safety: if a close action would affect more than 5 PRs, first ask for explicit user confirmation with the exact PR count and target scope/query.
