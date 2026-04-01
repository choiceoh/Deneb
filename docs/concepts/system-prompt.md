---
summary: "What the Deneb system prompt contains and how it is assembled"
read_when:
  - Editing system prompt text, tools list, or time/heartbeat sections
  - Changing workspace bootstrap or skills injection behavior
title: "System Prompt"
---

# System Prompt

Deneb builds a custom system prompt for every agent run. The prompt is **Deneb-owned** and does not use the pi-coding-agent default prompt.

The prompt is assembled by Deneb and injected into each agent run.

## Structure

The prompt is intentionally compact and uses fixed sections:

- **Identity**: agent name and persona ("You are Nev").
- **Communication**: communication style and language guidelines.
- **Attitude**: behavioral guidelines.
- **How to Act**: action-oriented behavior rules.
- **Trust and Respect**: trust relationship with the operator.
- **Tooling**: current tool list + short descriptions.
- **Tool Usage**: tool usage guidelines.
- **Skills** (when available): tells the model how to load skill instructions on demand.
- **Memory Recall** (when `memory` tool present): memory search guidance.
- **Polaris** (when `polaris` tool present): Polaris integration instructions.
- **Messaging**: message delivery behavior.
- **Session State**: current session context.
- **Context** (merged): workspace, date/time, context files, and runtime info.

Safety guardrails in the system prompt are advisory. They guide model behavior but do not enforce policy. Use tool policy, exec approvals, sandboxing, and channel allowlists for hard enforcement; operators can disable these by design.

## Prompt variants

Deneb has two prompt builder functions:

- `BuildSystemPrompt`: the standard prompt with all sections above.
- `BuildCodingSystemPrompt`: a variant for coding-focused sessions.

Both are defined in `gateway-go/internal/chat/prompt/system_prompt.go`.

## Workspace bootstrap injection

Bootstrap files are trimmed and appended under **Project Context** so the model sees identity and profile context without needing explicit reads:

- `CLAUDE.md`
- `SOUL.md`
- `TOOLS.md`
- `IDENTITY.md`
- `USER.md`
- `MEMORY.md` when present, otherwise `memory.md` as a lowercase fallback

All of these files are **injected into the context window** on every turn, which
means they consume tokens. Keep them concise — especially `MEMORY.md`, which can
grow over time and lead to unexpectedly high context usage and more frequent
compaction.

> **Note:** `memory/*.md` daily files are **not** injected automatically. They
> are accessed on demand via the `memory_search` and `memory_get` tools, so they
> do not count against the context window unless the model explicitly reads them.

Large files are truncated with a marker. The max per-file size is controlled by
`agents.defaults.bootstrapMaxChars` (default: 20000). Total injected bootstrap
content across files is capped by `agents.defaults.bootstrapTotalMaxChars`
(default: 150000). Missing files inject a short missing-file marker. When truncation
occurs, Deneb can inject a warning block in Project Context; control this with
`agents.defaults.bootstrapPromptTruncationWarning` (`off`, `once`, `always`;
default: `once`).

Sub-agent sessions only inject `AGENTS.md` and `TOOLS.md` (other bootstrap files
are filtered out to keep the sub-agent context small).

Internal hooks can intercept this step via `agent:bootstrap` to mutate or replace
the injected bootstrap files (for example swapping `SOUL.md` for an alternate persona).

To inspect how much each injected file contributes (raw vs injected, truncation, plus tool schema overhead), use `/context list` or `/context detail`. See [Context](/concepts/context).

## Time handling

The system prompt includes the current date, time, and timezone in the merged
**Context** section (e.g., "Monday, January 2, 2006 — 15:04 (timezone: Asia/Seoul)").

Use `session_status` when the agent needs the current time; the status card
includes a timestamp line.

Configure with:

- `agents.defaults.userTimezone`
- `agents.defaults.timeFormat` (`auto` | `12` | `24`)

See [Date & Time](/reference/date-time) for full behavior details.

## Skills

When eligible skills exist, Deneb injects a compact **available skills list**
(`formatSkillsForPrompt`) that includes the **file path** for each skill. The
prompt instructs the model to use `read` to load the SKILL.md at the listed
location (workspace, managed, or bundled). If no skills are eligible, the
Skills section is omitted.

```
<available_skills>
  <skill>
    <name>...</name>
    <description>...</description>
    <location>...</location>
  </skill>
</available_skills>
```

This keeps the base prompt small while still enabling targeted skill usage.

## Documentation

When available, the system prompt includes a **Documentation** section that points to the
local Deneb docs directory (either `docs/` in the repo workspace or the bundled npm
package docs) and also notes the public mirror, source repo, community Telegram, and
ClawHub ([https://clawhub.com](https://clawhub.com)) for skills discovery. The prompt instructs the model to consult local docs first
for Deneb behavior, commands, configuration, or architecture, and to run
`deneb status` itself when possible (asking the user only when it lacks access).
