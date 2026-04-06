---
name: skill-factory
version: "1.0.0"
category: coding
description: "Automatically extract reusable skills from complex workflows. Use when: (1) you just completed a multi-step task that could recur, (2) you notice a repeated pattern across sessions, (3) the user says 'make this a skill' or 'remember how to do this'. NOT for: one-off tasks, simple operations, or tasks already covered by existing skills."
metadata:
  {
    "deneb":
      {
        "emoji": "🏭",
        "tags": ["automation", "extraction", "pattern", "procedural-memory"],
        "related_skills": ["skill-creator", "skill-evolution"],
      },
  }
---

# Skill Factory — Autonomous Skill Extraction

Inspired by hermes-agent's procedural memory: the agent creates skills from experience.

## When to Use

**Trigger autonomously** after completing a complex workflow (5+ tool calls, multi-step reasoning):

1. You just solved a non-trivial problem with a reusable pattern
2. You notice you've done a similar workflow before (check session-logs)
3. The user explicitly asks to capture a workflow as a skill

**Do NOT create a skill when:**
- The task is a one-off with no reuse potential
- An existing skill already covers the workflow
- The pattern is too specific to generalize

## Extraction Procedure

### Step 1: Identify the Pattern

Review the just-completed workflow. Ask:
- What was the goal?
- What tools/commands were used?
- What decisions were non-obvious?
- What pitfalls did I encounter?
- Would a future agent benefit from these instructions?

### Step 2: Choose Category

| If the skill is about... | Category |
|---|---|
| Code, builds, testing, git, CI | `coding` |
| Daily workflows, docs, summarization | `productivity` |
| Server, monitoring, infra | `devops` |
| External APIs, bridges, imports | `integration` |

### Step 3: Generate SKILL.md

Create `skills/<category>/<name>/SKILL.md` with:

```yaml
---
name: extracted-skill-name
version: "1.0.0"
category: <category>
description: "<What it does>. Use when: <triggers>. NOT for: <anti-patterns>."
metadata:
  {
    "deneb":
      {
        "emoji": "<icon>",
        "tags": ["keyword1", "keyword2"],
        "related_skills": ["existing-skill"],
      },
  }
---
```

### Step 4: Write Body Sections

Follow the standard body structure:

1. **When to Use** — Concrete trigger conditions from the workflow
2. **Quick Reference** — The key commands/patterns extracted
3. **Procedure** — Step-by-step from the actual workflow, generalized
4. **Pitfalls** — Errors encountered and how to avoid them
5. **Verification** — How to confirm the workflow succeeded

### Step 5: Validate

- Does the skill add value beyond what's already in existing skills?
- Is the description specific enough for the LLM to know when to load it?
- Are the tags useful for discovery?
- Is the procedure generalizable (no hardcoded paths/values)?

## Nudging Protocol

After completing a complex task, briefly consider:

> "이 작업은 스킬로 만들 가치가 있는가?"

If yes, offer to the user: "이 워크플로우를 스킬로 저장할까요?" If the user agrees, execute the extraction procedure.

## Anti-Patterns

- Do NOT create skills that duplicate existing tool functionality
- Do NOT create skills with hardcoded credentials, paths, or user-specific data
- Do NOT create overly broad skills ("do everything") — narrow scope, deep quality
- Do NOT create skills for tasks the LLM already handles well without instructions
