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
- PR submission template (canonical): `.github/pull_request_template.md`
- Issue submission templates (canonical): `.github/ISSUE_TEMPLATE/`

## Git Operations & Safety

- **Stacked PRs: retarget the base to `main` before merging.** Merging a PR whose base is still the parent PR's branch lands the squash commit on that branch — not on main — while GitHub still shows the PR as MERGED. This silently dropped #2119/#2125/#2126 from main on 2026-06-09 (only #2112, whose base was main, actually landed).
- **MERGED state is not proof of landing.** After merging, verify the change is on main: `git merge-base --is-ancestor <mergeCommitSHA> origin/main` (or grep main for the change). Apply this check whenever confirming "merge complete" for stacked or multi-PR work.
- If `git branch -d/-D <branch>` is policy-blocked, delete the local ref directly: `git update-ref -d refs/heads/<branch>`.
- Agents MUST NOT create or push merge commits on `main`. If `main` has advanced, rebase local commits onto the latest `origin/main` before pushing.
- Bulk PR close/reopen safety: if a close action would affect more than 5 PRs, first ask for explicit user confirmation with the exact PR count and target scope/query.
