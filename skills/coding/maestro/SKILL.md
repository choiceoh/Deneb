---
name: maestro
version: "1.0.0"
category: coding
description: "Orchestrate multi-skill workflows with plan-execute-verify lifecycle. Use when: (1) a task requires chaining 2+ skills in sequence, (2) complex operations need explicit planning before execution, (3) long-running tasks need lifecycle tracking and recovery. NOT for: single-skill tasks, simple tool calls, or tasks that don't benefit from explicit planning."
metadata:
  {
    "deneb":
      {
        "emoji": "🎼",
        "tags": ["orchestration", "planning", "composition", "lifecycle", "multi-step"],
        "related_skills": ["coding-agent", "skill-factory"],
      },
  }
---

# Maestro — Skill Orchestration

Inspired by hermes-agent's maestro pattern: Conductor plans, individual skills execute.

## When to Use

- Task requires **2+ skills** to complete (e.g., github + coding-agent + summarize)
- Task has **dependencies** between steps (step 2 needs step 1's output)
- Task is **long-running** and needs checkpoint/recovery capability
- User asks for a **complex workflow** that spans multiple domains

## Orchestration Protocol

### Phase 1: Plan (Conductor)

Before executing, create an explicit plan:

```
## Execution Plan
1. [skill: github] — Fetch PR diff and review comments
2. [skill: coding-agent] — Apply requested changes
3. [skill: github] — Push and update PR status
4. [verify] — Check CI status
```

Rules:
- Each step names the **skill** it will use (or "tool" for direct tool calls)
- Steps with dependencies are marked: `[depends: step 1]`
- Verification checkpoints are explicit `[verify]` steps
- Estimate complexity: simple (1-2 tools) / moderate (3-5) / complex (5+)

### Phase 2: Execute (Sequential)

For each step in the plan:

1. **Load** the skill: read the SKILL.md for the step's skill
2. **Execute** following the skill's procedure
3. **Capture** the output/result for dependent steps
4. **Verify** intermediate result before proceeding

If a step fails:
- Log the failure reason
- Check if the plan can continue (skip optional steps)
- If critical, halt and report to user with recovery options

### Phase 3: Verify (Quality Gate)

After all steps complete:

1. Review each step's output against the original goal
2. Run any skill-specific verification procedures
3. Summarize results to user

## Composition Patterns

### Sequential Chain
```
skill-A → skill-B → skill-C
```
Each skill's output feeds the next. Use when order matters.

### Fan-Out / Fan-In
```
        → skill-B →
skill-A              skill-D
        → skill-C →
```
Parallel independent steps, then merge results. Use for independent subtasks.

### Conditional Branch
```
skill-A → [if condition] → skill-B
                         → skill-C
```
Choose next skill based on previous output. Use for adaptive workflows.

## Skill Dependency Resolution

When a skill declares `related_skills` in its metadata, maestro can:
1. Pre-load related skills for context
2. Chain related skills when the workflow suggests it
3. Suggest missing skills that would improve the workflow

## Error Recovery

| Failure Type | Recovery Strategy |
|---|---|
| Skill not found | Check related_skills, suggest alternatives |
| Tool error in step | Retry once, then skip if optional, halt if critical |
| Dependency failed | Skip all dependent steps, report partial results |
| Timeout | Save checkpoint, offer to resume |

## Anti-Patterns

- Do NOT orchestrate when a single skill suffices
- Do NOT create deep dependency chains (max 5 sequential steps)
- Do NOT retry indefinitely — fail fast, report clearly
- Do NOT hide errors from the user — transparency over polish
