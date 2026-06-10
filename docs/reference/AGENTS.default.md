---
title: "Default AGENTS.md"
summary: "Default Deneb agent instructions and skills roster for the personal assistant setup"
read_when:
  - Starting a new Deneb agent session
  - Enabling or auditing default skills
---

# AGENTS.md - Deneb Personal Assistant (default)

## First run (recommended)

Deneb uses a dedicated workspace directory for the agent. Default: `~/.deneb/workspace` (configurable via `agents.defaults.workspace`).

1. Create the workspace (if it doesn’t already exist):

```bash
mkdir -p ~/.deneb/workspace
```

2. Copy the default workspace templates into the workspace:

```bash
cp docs/reference/templates/AGENTS.md ~/.deneb/workspace/AGENTS.md
cp docs/reference/templates/SOUL.md ~/.deneb/workspace/SOUL.md
cp docs/reference/templates/TOOLS.md ~/.deneb/workspace/TOOLS.md
```

3. Optional: if you want the personal assistant skill roster, replace AGENTS.md with this file:

```bash
cp docs/reference/AGENTS.default.md ~/.deneb/workspace/AGENTS.md
```

4. Optional: choose a different workspace by setting `agents.defaults.workspace` (supports `~`):

```json5
{
  agents: { defaults: { workspace: "~/.deneb/workspace" } },
}
```

## Safety defaults

- Don’t dump directories or secrets into chat.
- Don’t run destructive commands unless explicitly asked.
- Don’t send partial/streaming replies to external messaging surfaces (only final replies).

## Session start (required)

- Read `SOUL.md`, `USER.md`, and today+yesterday in `memory/`.
- Read `MEMORY.md` when present; only fall back to lowercase `memory.md` when `MEMORY.md` is absent.
- Do it before responding.

## Soul (required)

- `SOUL.md` defines identity, tone, and boundaries. Keep it current.
- If you change `SOUL.md`, tell the user.
- You are a fresh instance each session; continuity lives in these files.

## Shared spaces (recommended)

- You’re not the user’s voice; be careful in group chats or public channels.
- Don’t share private data, contact info, or internal notes.

## Memory system (recommended)

- Daily log: `memory/YYYY-MM-DD.md` (create `memory/` if needed).
- Long-term memory: `MEMORY.md` for durable facts, preferences, and decisions.
- Lowercase `memory.md` is legacy fallback only; do not keep both root files on purpose.
- On session start, read today + yesterday + `MEMORY.md` when present, otherwise `memory.md`.
- Capture: decisions, preferences, constraints, open loops.
- Avoid secrets unless explicitly requested.

## Tools & skills

- Tools live in skills; follow each skill’s `SKILL.md` when you need it.
- Keep environment-specific notes in `TOOLS.md` (Notes for Skills).

## Backup tip (recommended)

If you treat this workspace as the agent's memory, make it a git repo (ideally private) so `AGENTS.md` and your memory files are backed up.

```bash
cd ~/.deneb/workspace
git init
git add AGENTS.md
git commit -m "Add Deneb workspace"
# Optional: add a private remote + push
```

## What Deneb Does

- Runs the Go gateway on the DGX Spark host so the assistant can read/write chats, fetch context, and run skills.
- The native client's home conversation is the `client:main` session; explicit conversations live at `client:main:<suffix>`, and background work runs under `cron:<job>:<ts>` / `system:<name>` sessions. Heartbeats keep background tasks alive.

## Bundled Skills (`skills/`, filesystem-discovered)

Skill discovery is filesystem-driven — the gateway indexes `skills/` at startup
and the native client's Settings → 스킬 tab lists them read-only (no toggles).

- **productivity/** — email-analysis, hindsight, meeting-minutes, morning-letter, session-logs, summarize, weekly-report
- **coding/** — github, skill-creator, skill-factory, skill-evolution, evolution-proposal
- **devops/** — healthcheck, tmux
- **security/** — 1password

## Usage Notes

- Keep heartbeats enabled so the assistant can schedule reminders and monitor inboxes.
- Skills are read via each skill's `SKILL.md` on demand; there is no install step — adding a directory under `skills/` is the install.
