---
name: evolution-proposal
version: "1.0.7"
category: coding
description: "Propose, record, and execute self-evolution after a meaningful workflow via the skill_lifecycle tool. Use when: (1) a completed task may deserve a reusable skill, (2) the user asks for skill genesis, self-evolution, or an evolution proposal, (3) an existing skill should be evolved instead of creating a new one. NOT for: ordinary coding work, one-off notes, or directly authoring a SKILL.md without first deciding the route."
metadata:
  {
    "deneb":
      {
        "emoji": "🧭",
        "tags": ["self-evolution", "genesis", "proposal", "procedural-memory", "routing", "SkillOpt", "Self-Harness", "held-out-replay", "self-correction-queue"],
        "related_skills": ["skill-factory", "skill-creator", "skill-evolution"],
      },
  }
---

# Evolution Proposal

Lightweight entry point for Deneb's skill lifecycle. Inspired by Hermes'
`evolution_proposal` and Self-Harness: decide whether recent experience should
become a new skill, improve an existing skill, or be ignored, then keep only
evidence-grounded, validation-gated changes.

## When to Use

Use this skill after a non-trivial workflow, especially when one of these is true:

- The task used 2+ tool calls and 2+ agent turns, or a successful `code_action`
  compressed a reusable batch/join/normalization/internal-write workflow into
  one tool call.
- The user says "skill genesis", "self-evolution", "evolution proposal", "자기진화", "스킬화", or asks whether Deneb should learn from the workflow.
- The workflow exposed a repeated pitfall, missing procedure, or reusable command sequence.
- The user corrected scope, response format, validation order, or "how agents should work" in a way future agents should follow.
- A generated/managed skill exists, but its instructions are stale or incomplete.

Do not use this for one-off facts, durable user preferences, secrets, or simple
commands. Those belong in wiki/memory or nowhere, not in a skill.

## Decision Route

Use the Hermes mainline decision order, not a "new skill first" bias:

1. Patch or evolve the currently loaded/closest existing skill.
2. Add the rule to an existing umbrella skill.
3. Add a support artifact under an existing skill (`references`, `templates`, `scripts`, or `assets`) when detailed commands/config are the durable knowledge. Use `skills` action `write_file`; do not bury long command matrices in the main SKILL.md if a reference file is cleaner.
4. Create/genesis a new class-level skill only when no existing skill owns the pattern.

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
Use `status` when you need to check recent proposal/genesis history before
deciding, to inspect usage stats, or to see curator state for agent-created
skills before evolving/duplicating one. Read `rejectedEdits` in the status
output before proposing another evolve route; they are failed candidate patches
that should not be repeated. Read `validationCases` as held-out replay tests
that future evolved candidates must still satisfy. Read `selfCorrectionCandidates`
as the deferred queue of unapplied correction ideas for a future coding agent to
batch-review. Use `pin`, `unpin`,
`archive`, or `restore` only for agent-created skills whose curator state needs
explicit operator control.

## Procedure

1. State the candidate pattern in one sentence.
2. Check existing skills with `skills` action `list`; read the closest match if any.
3. If a close match exists, prefer `Evolve`; if detailed config/code snippets are the reusable part, preserve them inside the existing skill or a support file.
4. Decide the route using the table above.
5. Load `skill_lifecycle` with `fetch_tools` if the schema is not visible.
6. If the session history is unclear, call `skill_lifecycle` action `status` first and review recent lifecycle records plus `selfCorrectionCandidates`.
7. Record the decision with `skill_lifecycle` action `propose`.
8. If route is `Genesis`, pass `execute=true` or call action `genesis`; omit `sessionKey` to use the current session, or pass a concise `dreamSummary`.
9. If route is `Evolve`, pass `execute=true` with `skillName` and put the concrete improvement directive in `reason`/`evidence`; tie the directive to one supported failure mechanism, editable surface, expected behavior change, and regression risk. The evolver will persist those Self-Harness audit fields when the candidate supplies them. For direct action `evolve`, pass the same directive as `finding`.
10. If the reusable workflow is being implemented with `code_action`, pass `promoteToSkill` in the same call once the pattern is clear. It records a `skill_lifecycle` proposal after the Python run succeeds and skips promotion when the script fails.
11. If route is `Create`, load `skill-factory` and create a concise `SKILL.md` with `skills` action `create`.
12. If the durable detail is a command/config/code reference, add it with `skills` action `write_file` under `references/`, `templates/`, `scripts/`, or `assets/`.
13. If the task exposed a concrete failure that can be replayed from a stored transcript, record it with `skill_lifecycle` action `validation_case_from_session` before or after evolve; pass `skillName`, `sessionKey`, and a short `description`. Use manual `validation_case` only when the invariant needs extra replay fields (`input`, `requiredActions`, `forbiddenActions`, `expectedToolCalls`, `forbiddenToolCalls`, `requiredObservations`, `forbiddenObservations`, `requireOrder`) that the transcript cannot prove by itself.
14. If you notice a plausible correction but cannot safely apply and validate it now, record it with `skill_lifecycle` action `self_correction` using `title`, `evidence`, `targetFiles`, `proposedChange`, and `risk`; do not mutate files in that action.
15. For executed `Genesis`/`Evolve` routes, call `skill_lifecycle` action `status` with `limit: 5` when you need an audit trail.
16. Report what changed, what was only queued, or why no change was made.

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

Typical `code_action` promotion after a successful reusable batch workflow:

```json
{
  "code": "rows = deneb.contacts(\"search\", \"탑솔라\", as_json=True)\nprint(len(rows))",
  "promoteToSkill": {
    "candidate": "Use code_action to batch join structured contacts/calendar/wiki data before responding",
    "evidence": "successful structured as_json join; reusable for multi-source business analysis",
    "route": "genesis",
    "execute": true
  }
}
```

Typical session-extracted held-out replay case:

```json
{
  "action": "validation_case_from_session",
  "skillName": "srv1-ops",
  "sessionKey": "client:main:srv1-maintenance",
  "id": "inspect-real-server-before-change",
  "description": "Do not optimize from local assumptions when the user asks for srv1 state.",
  "replay": {
    "requiredActions": ["ssh srv1", "systemctl --user status deneb-gateway.service"],
    "requiredObservations": ["active (running)"]
  },
  "source": "review-finding"
}
```

Typical manual held-out replay case:

```json
{
  "action": "validation_case",
  "skillName": "srv1-ops",
  "id": "inspect-real-server-before-change",
  "description": "Do not optimize from local assumptions when the user asks for srv1 state.",
  "replay": {
    "input": "Tailscale SSH into srv1, inspect deneb-gateway, then improve from the real state.",
    "requiredActions": ["ssh srv1", "systemctl --user status deneb-gateway.service"],
    "forbiddenActions": ["assume local health is production health"],
    "requiredObservations": ["active (running)"],
    "forbiddenObservations": ["stopped"],
    "expectedToolCalls": [
      {"name": "exec", "inputIncludes": ["ssh srv1"]},
      {
        "name": "exec",
        "inputIncludes": ["systemctl --user status deneb-gateway.service"],
        "fixtureOutput": "Active: active (running)"
      }
    ],
    "forbiddenToolCalls": [
      {"name": "exec", "inputIncludes": ["rm -rf"]}
    ],
    "requireOrder": true
  },
  "source": "operator"
}
```

## Pitfalls

- Do not create a skill just because a task was long. The workflow must be reusable.
- Do not duplicate `skill-factory`, `skill-creator`, or `skill-evolution`; route to them.
- Do not store secrets, private contact data, or single-session context in a skill.
- Do not name new skills after PR numbers, exact errors, codenames, or one session's artifact; make the name class-level.
- Do not mutate a skill and invalidate prompt cache mid-session unless immediate use is necessary; prefer deferred application.
- Do not silently leave a useful correction in chat only; if it should be revisited by a coding agent, queue it with `skill_lifecycle` action `self_correction`.
- Do not widen narrow chat presets just to expose lifecycle tools; if the current surface lacks `skill_lifecycle`, state the intended proposal route and stop there.
- Do not confuse skills with memory/wiki: skills are reusable procedures; memory/wiki stores durable facts or personal context.
- Do not put support files outside `references/`, `templates/`, `scripts/`, or `assets/`; those directories are the safe support-file surface.
- Do not repeat an evolve candidate that already appears in `rejectedEdits`; propose a smaller or differently scoped patch instead.
- Do not make replay cases so broad that every candidate fails; each case should protect one concrete regression.
- Do not propose a speculative evolution when the failure evidence is weak or the failure is not addressable by a skill surface; record a no-op or validation case instead.

## Verification

- New skill: `skills` action `list` can discover it, and its description has concrete triggers.
- Evolved skill: the patch is narrow, version is bumped when appropriate, and the original purpose remains intact.
- Genesis route: `skill_lifecycle` reports either a created skill, a skip reason, or a clear error.
- Proposal route: the result includes `route` and `executed`, so the loop is auditable.
- Code-action promotion: `code_action` output includes `[code_action skill promotion]`, and `skill_lifecycle` status shows the proposal/genesis record.
- Audit route: `skill_lifecycle` action `status` shows recent proposal/genesis records, usage stats, rejected edits, validation cases, and curator state for agent-created skills.
- Deferred correction route: `skill_lifecycle` status shows the candidate in `selfCorrectionCandidates` until a reviewer marks it accepted/rejected/superseded/applied with `self_correction_review`.

## Changelog
- v1.0.7: Added deferred self-correction queue guidance.
- v1.0.6: Noted persisted Self-Harness audit fields for evolve routes.
- v1.0.5: Added Self-Harness evidence-grounding for evolve proposals.
- v1.0.4: Added `code_action.promoteToSkill` route for successful reusable code workflows.
- v1.0.3: Added session-extracted validation case guidance.
- v1.0.2: Added held-out replay validation case guidance.
- v1.0.1: Added SkillOpt-style rejected-edit status checks and direct evolve finding guidance.
