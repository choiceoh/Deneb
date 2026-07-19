---
name: evolution-proposal
version: "1.0.9"
category: coding
description: "Propose, record, and execute self-evolution after a meaningful workflow via the skill_lifecycle tool. Use when: (1) a completed task may deserve a reusable skill, (2) the user asks for skill genesis, self-evolution, or an evolution proposal, (3) an existing skill should be evolved instead of creating a new one. NOT for: ordinary coding work, one-off notes, or directly authoring a SKILL.md without first deciding the route."
metadata:
  {
    "deneb":
      {
        "emoji": "🧭",
        "tags": ["self-evolution", "genesis", "proposal", "procedural-memory", "routing", "SkillOpt", "Self-Harness", "held-out-replay", "self-correction-queue"],
        "triggers": ["자가개선", "자기진화", "스킬화", "스킬 생성", "스킬 개선", "스킬 진화", "evolution proposal"],
        "related_skills": ["skill-factory"],
        "requires_tools": ["skill_lifecycle", "skills"],
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
| Evolve | An existing skill almost covers the workflow | Call `skill_lifecycle` action `evolve` |

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

## Minimum Process

Keep the dependency order, but do not run every possible lifecycle action as one
long checklist:

1. **Frame and route.** State one candidate and its evidence. Use `skills` action
   `list`, read only the closest match, and choose exactly one route. If history
   or duplicate risk is unclear, inspect `skill_lifecycle` action `status`,
   including `rejectedEdits`, `validationCases`, and `selfCorrectionCandidates`.
2. **Record and execute once.** Load `skill_lifecycle` with `fetch_tools` only
   when its schema is not visible, record action `propose`, then execute only the
   chosen route:
   - **Genesis:** pass `execute=true` to `propose` or call action `genesis`;
     omit `sessionKey` for the current session, or pass a concise `dreamSummary`.
   - **Evolve:** pass `execute=true` with `skillName` and a concrete directive in
     `reason`/`evidence`. Tie it to one supported failure mechanism, editable
     surface, expected behavior change, and regression risk. For direct action
     `evolve`, pass the same directive as `finding`.
   - **Create:** load `skill-factory`, then use `skills` action `create` for a
     concise `SKILL.md`.
   - **No-op:** report why existing coverage is sufficient.
   - When a successful `code_action` itself proves the reusable workflow, pass
     `promoteToSkill` in that same call. It records the proposal only after the
     Python run succeeds.
3. **Keep only needed follow-up.** Put durable command/config/code detail under
   `references/`, `templates/`, `scripts/`, or `assets/` with `skills` action
   `write_file`. Add `validation_case_from_session` only for a replayable failure;
   use manual `validation_case` only for extra invariants the transcript cannot
   prove. Queue unsafe or deferred code changes with action `self_correction` and
   its evidence/target/risk fields; do not mutate files in that action. Query
   `status` with `limit: 5` only when an audit trail is needed.
4. **Report the outcome.** Say what changed, what was queued, or why it was a
   no-op.

### Next-State Feedback

Treat user corrections, PR review comments, failed tests, tool errors, and
post-merge/deploy reality checks as next-state evidence. Convert only the
actionable part into one of:

- `validation_case_from_session` when the transcript can replay the invariant.
- `self_correction` when a future coding agent should inspect code, prompts,
  docs, config, or tests before applying anything.
- no-op when the signal is preference-only, already covered, or not
  reproducible.

Do not present this as model training. In Deneb it is an auditable queue:
external feedback -> structured hint -> batch review -> focused validation.

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
- Do not duplicate `skill-factory`; route to it.
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
- v1.0.9: Replaced the incidental 16-step linear checklist with a four-part minimum path and route-specific actions; preserved lifecycle, replay, and audit gates.
- v1.0.8: Added precise auto-surfacing triggers and just-in-time activation for lifecycle tools; this skill now owns the detailed Propus procedure instead of the ambient system prompt.
- v1.0.7: Added deferred self-correction queue guidance.
- v1.0.6: Noted persisted Self-Harness audit fields for evolve routes.
- v1.0.5: Added Self-Harness evidence-grounding for evolve proposals.
- v1.0.4: Added `code_action.promoteToSkill` route for successful reusable code workflows.
- v1.0.3: Added session-extracted validation case guidance.
- v1.0.2: Added held-out replay validation case guidance.
- v1.0.1: Added SkillOpt-style rejected-edit status checks and direct evolve finding guidance.
