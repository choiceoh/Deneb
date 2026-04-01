---
description: "Git 워크플로우, PR, 커밋 상세 가이드라인"
globs: [".github/**", "scripts/committer"]
---

# Git Commit & PR Details

## Commit & Pull Request Guidelines

- Use `$deneb-pr-maintainer` at `.agents/skills/deneb-pr-maintainer/SKILL.md` for maintainer PR triage, review, close, search, and landing workflows.
- This includes auto-close labels, bug-fix evidence gates, GitHub comment/search footguns, and maintainer PR decision flow.
- `/landpr` lives in the global Codex prompts (`~/.codex/prompts/landpr.md`); when landing or merging any PR, always follow that `/landpr` process.
- Create commits with `scripts/committer "<msg>" <file...>`; avoid manual `git add`/`git commit` so staging stays scoped.
- Follow concise, action-oriented commit messages.
- Group related changes; avoid bundling unrelated refactors.
- Before landing or merging a PR, verify CI status: `scripts/build-status pr <N>` or use MCP `pull_request_read(method="get_check_runs")`.
- PR submission template (canonical): `.github/pull_request_template.md`
- Issue submission templates (canonical): `.github/ISSUE_TEMPLATE/`

## Git Operations & Safety

- If `git branch -d/-D <branch>` is policy-blocked, delete the local ref directly: `git update-ref -d refs/heads/<branch>`.
- Agents MUST NOT create or push merge commits on `main`. If `main` has advanced, rebase local commits onto the latest `origin/main` before pushing.
- Bulk PR close/reopen safety: if a close action would affect more than 5 PRs, first ask for explicit user confirmation with the exact PR count and target scope/query.
