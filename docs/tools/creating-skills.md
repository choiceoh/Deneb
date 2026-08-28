---
title: "Creating Skills"
summary: "Build and test custom workspace skills with SKILL.md"
read_when:
  - You are creating a new custom skill in your workspace
  - You need a quick starter workflow for SKILL.md-based skills
---

# Creating Custom Skills 🛠

Deneb is designed to be easily extensible. "Skills" are the primary way to add new capabilities to your assistant.

## What is a Skill?

A skill is a directory containing a `SKILL.md` file (which provides instructions and tool definitions to the LLM) and optionally some scripts or resources.

## Step-by-Step: Your First Skill

### 1. Create the Directory

Skills are discovered from four roots (later overrides earlier for the same name):
managed `~/.deneb/skills/`, repo `skills/`, workspace `skills/`, and
`~/.deneb/workspace/skills/`. Create a new folder for your skill:

```bash
mkdir -p ~/.deneb/workspace/skills/hello-world
```

### 2. Define the `SKILL.md`

Create a `SKILL.md` file in that directory. This file uses YAML frontmatter for metadata and Markdown for instructions.

```markdown
---
name: hello_world
description: A simple skill that says hello.
---

# Hello World Skill

When the user asks for a greeting, use the plain reply text (there is no `echo` tool) to say "Hello from your custom skill!".
```

### 3. Add Tools (Optional)

You can define custom tools in the frontmatter or instruct the agent to use existing system tools (like `exec` or `web`).

### 4. Refresh Deneb

Ask your agent to "refresh skills" or restart the gateway. Deneb will discover the new directory and index the `SKILL.md`.

## Best Practices

- **Be Concise**: Instruct the model on _what_ to do, not how to be an AI.
- **Safety First**: If your skill guides the agent to use `exec`, ensure the prompts don't allow arbitrary command injection from untrusted user input.
- **Test Locally**: Use `scripts/dev/live-test.sh chat "use my new skill"` against the dev gateway to test.

## Body Craft: Eight Disciplines for Skill Prose

Authoring discipline that makes small-to-mid local models execute a skill
accurately. Distilled 2026-08 from a clean-room study of a production
local-agent skill corpus (verdict record: `perplexity-portable-computer-review`
in the operator memory's closed-reviews index) — principles only, no text
imported. The measured background: a small model rarely _disobeys_ a skill; it
_fails to satisfy_ one whose body isn't self-serve. Facts, tables, and budgets
outperform exhortation.

1. **State the call budget up front.** Right after the workflow overview, pin
   the intended tool-call count ("explore once if there's an input, then
   build + verify in one more call: target 2"). Without a budget the model
   pokes the shell ten times even when a helper exists.
2. **Block the one fatal mistake with a leading blockquote.** Every domain
   has one error the model _will_ make; preempt exactly that as a quote at
   the top (for example "> The .xlsx is a binary produced by running Python.
   Never patch it with `edit`").
3. **Helper first, hand-rolling as fallback.** If the skill bundles a
   `scripts/` helper, the body teaches the one-command helper FIRST and
   demotes library-level hand implementation to "only after the helper
   failed". The skill-plus-script bundle is the product; the prose is its
   manual.
4. **Declare environment truth as fact.** What exists and what doesn't, in
   the indicative: "openpyxl is in the venv. There is no soffice, so a
   formula recalculation step does not exist." Warning prose ("be careful")
   is not a discipline.
5. **One WRONG/RIGHT pair, on the single most important rule.** On every rule
   it is noise; on none, the rule slips.
6. **Lookup knowledge goes in tables.** Format codes, layout positions,
   alignment norms, tool selection: tables, not paragraphs.
7. **Name API traps at identifier granularity.** List the exact places
   models fumble (kwarg spellings, long/short naming splits within one
   library).
8. **End with a delivery gate.** Completion may be declared only after the
   success signal was actually observed (printed output path, file size,
   reopen and inspect): never announce a deliverable you have not seen, and
   ask before overwriting an existing file. In Deneb, delivery includes
   handing the artifact over via `send_file`.
