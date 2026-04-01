---
description: "협업, 보안, 멀티 에이전트 안전 규칙"
globs: ["**"]
---

# Collaboration & Safety

- When working on a GitHub Issue or PR, print the full URL at the end of the task.
- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- Patching dependencies requires explicit approval; do not do this by default.

## Multi-Agent Safety

- **Do not create/apply/drop `git stash` entries** unless explicitly requested (this includes `git pull --rebase --autostash`). Assume other agents may be working; keep unrelated WIP untouched and avoid cross-cutting state changes.
- **When the user says "push"**, you may `git pull --rebase` to integrate latest changes (never discard other agents' work). When the user says "commit", scope to your changes only. When the user says "commit all", commit everything in grouped chunks.
- **Do not create/remove/modify `git worktree` checkouts** (or edit `.worktrees/*`) unless explicitly requested.
- **Do not switch branches / check out a different branch** unless explicitly requested.
- **Running multiple agents is OK** as long as each agent has its own session.
- **When you see unrecognized files**, keep going; focus on your changes and commit only those.

## Code Quality & Safety

- Lint/format churn:
  - If staged+unstaged diffs are formatting-only, auto-resolve without asking.
  - If commit/push already requested, auto-stage and include formatting-only follow-ups in the same commit (or a tiny follow-up commit if needed), no extra confirmation.
  - Only ask when changes are semantic (logic/data/behavior).
- **CI status check:** Before pushing, check CI status of main (`scripts/build-status main` or MCP tools per `.claude/rules/build-status.md`). After pushing, verify your branch's CI status. Report the result to the user.
- **Focus reports on your edits**; avoid guard-rail disclaimers unless truly blocked; when multiple agents touch the same file, continue if safe; end with a brief "other files present" note only if relevant.
- Bug investigations: read source code and all related local code before concluding; aim for high-confidence root cause.
- Code style: add brief comments for tricky logic; keep files under ~500 LOC when feasible (split/refactor as needed).
- Never send streaming/partial replies to external messaging surfaces (WhatsApp, Telegram); only final replies should be delivered there. Streaming/tool events may still go to internal UIs/control channel.
- For manual `deneb message send` messages that include `!`, use the heredoc pattern to avoid the Bash tool's escaping.
- Release guardrails: do not change version numbers without operator's explicit consent.

## Security & Configuration

- Web provider stores creds at `~/.deneb/credentials/`; rerun `deneb login` if logged out.
- Pi sessions live under `~/.deneb/sessions/` by default; the base directory is not configurable.
- Environment variables: see `~/.profile`.
- Never commit or publish real phone numbers, videos, or live configuration values. Use obviously fake placeholders in docs, tests, and examples.
- Release flow: use the private [maintainer release docs](https://github.com/deneb/maintainers/blob/main/release/README.md) for the actual runbook, `docs/reference/RELEASING.md` for the public release policy, and `$deneb-release-maintainer` for the maintainership workflow.
