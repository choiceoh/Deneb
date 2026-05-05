---
name: evolution-proposal
version: "1.0.0"
category: coding
description: "Propose, record, and execute self-evolution after a meaningful workflow via the skill_lifecycle tool. Use when: (1) a completed task may deserve a reusable skill, (2) the user asks for skill genesis, self-evolution, or an evolution proposal, (3) an existing skill should be evolved instead of creating a new one. NOT for: ordinary coding work, one-off notes, or directly authoring a SKILL.md without first deciding the route."
metadata:
  {
    "deneb":
      {
        "emoji": "🧭",
        "tags": ["self-evolution", "genesis", "proposal", "procedural-memory", "routing"],
        "related_skills": ["skill-factory", "skill-creator", "skill-evolution"],
      },
  }
---

# Evolution Proposal

Lightweight entry point for Deneb's skill lifecycle. Inspired by Hermes'
`evolution_proposal`: decide whether recent experience should become a new
skill, improve an existing skill, or be ignored.

## When to Use

Use this skill after a non-trivial workflow, especially when one of these is true:

- The task used 5+ tool calls or 3+ agent turns.
- The user says "skill genesis", "self-evolution", "evolution proposal", "자기진화", "스킬화", or asks whether Deneb should learn from the workflow.
- The workflow exposed a repeated pitfall, missing procedure, or reusable command sequence.
- A generated/managed skill exists, but its instructions are stale or incomplete.

Do not use this for one-off facts, durable user preferences, secrets, or simple
commands. Those belong in wiki/memory or nowhere, not in a skill.

## Decision Route

Choose exactly one route:

| Route | Use when | Action |
|---|---|---|
| No-op | The workflow is one-off or already covered | Say no skill change is needed |
| Genesis | A complete recent session has a reusable pattern | Call `skill_lifecycle` action `genesis` |
| Create | RPC is unavailable, but the pattern is clear now | Use `skill-factory`, then `skills` action `create` |
| Evolve | An existing skill almost covers the workflow | Call `skill_lifecycle` action `evolve` or use `skill-evolution` for a manual patch |

Prefer `Genesis` through `skill_lifecycle` when available; it preserves the
engine's cooldowns, duplicate checks, daily cap, generated-skill metadata, and
proposal logs. If the current agent surface cannot call that tool directly, be
explicit and fall back to `Create` or `Evolve` rather than pretending genesis ran.

## Procedure

1. State the candidate pattern in one sentence.
2. Check existing skills with `skills` action `list`; read the closest match if any.
3. Decide the route using the table above.
4. Load `skill_lifecycle` with `fetch_tools` if the schema is not visible.
5. Record the decision with `skill_lifecycle` action `propose`.
6. If route is `Genesis`, pass `execute=true` or call action `genesis`; omit `sessionKey` to use the current session, or pass a concise `dreamSummary`.
7. If route is `Evolve`, pass `execute=true` with `skillName` or call action `evolve`.
8. If route is `Create`, load `skill-factory` and create a concise `SKILL.md` with `skills` action `create`.
9. Report what changed, or why no change was made.

## Proposal Template

Use this compact structure when explaining the decision:

```text
Candidate: <reusable pattern>
Evidence: <tool/turn/pitfall signal>
Existing coverage: <none | skill-name>
Route: <No-op | Genesis | Create | Evolve>
Next action: <specific tool/RPC/patch or none>
```

Typical execution call after deciding `Genesis`:

```json
{
  "action": "propose",
  "candidate": "Reusable workflow pattern",
  "evidence": "5+ tool calls, repeated pitfall, user asked to keep it",
  "route": "genesis",
  "execute": true
}
```

## Pitfalls

- Do not create a skill just because a task was long. The workflow must be reusable.
- Do not duplicate `skill-factory`, `skill-creator`, or `skill-evolution`; route to them.
- Do not store secrets, private contact data, or single-session context in a skill.
- Do not mutate a skill and invalidate prompt cache mid-session unless immediate use is necessary; prefer deferred application.

## Verification

- New skill: `skills` action `list` can discover it, and its description has concrete triggers.
- Evolved skill: the patch is narrow, version is bumped when appropriate, and the original purpose remains intact.
- Genesis route: `skill_lifecycle` reports either a created skill, a skip reason, or a clear error.
- Proposal route: the result includes `route` and `executed`, so the loop is auditable.
