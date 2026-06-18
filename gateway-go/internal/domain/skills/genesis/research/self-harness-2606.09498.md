---
title: "Self-Harness 2606.09498 Analysis"
summary: "Analysis of Self-Harness and the Deneb skill-lifecycle changes it implies: failure evidence bundles, bounded edits, held-out validation, and auditable harness transitions."
read_when:
  - "Changing Deneb skill genesis/evolution, skill_lifecycle, or harness self-improvement gates"
  - "Deciding whether a self-improvement candidate should be accepted, rejected, or recorded as a validation case"
  - "Reviewing why Deneb clusters recent skill failures before rewriting SKILL.md"
sidebarTitle: "Self-Harness"
---

# Self-Harness (arXiv:2606.09498v1) - Deneb 적용 분석

Source: <https://arxiv.org/html/2606.09498v1>
Read date: 2026-06-18

## One-line take

Self-Harness is not "let the agent rewrite itself." The useful part is narrower:
turn execution evidence into a bounded harness edit, then promote it only if a
fixed evaluator says it improves without regression.

For Deneb, the closest production surface is the skill lifecycle. `SKILL.md` is
trainable external harness state for a frozen agent; skill evolution already has
rejected-edit memory, held-out replay cases, self-test/teacher gates, and
rollback. The missing piece was the paper's first stage: structured failure
mining before proposal.

## Paper mechanics

The loop has three stages:

1. **Weakness Mining** - run the current harness, collect traces with verifier
   outcomes, and cluster failures by a reusable signature: terminal verifier
   cause, causal status of the agent behavior, and the agent-side mechanism.
   This avoids treating one failed run as a design signal.
2. **Harness Proposal** - ask the same fixed model, under the current harness,
   to propose bounded and materially distinct edits. Each candidate must name
   the failure mechanism, editable surface, expected behavior change, and
   regression risk.
3. **Proposal Validation** - evaluate current vs candidate on held-in and
   held-out splits. Accept only if one split improves and the other does not
   degrade. Rejected candidates remain logged, not silently forgotten.

The experiments keep model, tool set, budget, environment, and evaluator fixed.
That isolation matters: observed improvement is attributed to harness state, not
model replacement.

Headline result: all three tested backends improved on held-out tasks after
validation-gated harness edits. The retained edits were small and model-specific:
early artifact creation, structured tool-output handling, dependency prechecks,
retry discipline, persistent shell environment, and faster transition from
exploration to implementation/testing.

## What transfers to Deneb

Directly transferable:

- **Evidence bundle before rewrite.** The evolver should see clustered failure
  mechanisms, not only recent raw error strings.
- **Addressability gate.** A failure only deserves a skill edit if a skill
  surface can plausibly fix it. Model capability limits, flaky infra, and weak
  one-off evidence should produce no-op or validation-case capture.
- **Small editable surfaces.** Prefer `Procedure`, `Pitfalls`, `Verification`,
  tags, or a support file over a whole-skill rewrite.
- **Auditable transition.** Candidate descriptions should include target
  failure signature, edited surface, expected behavior change, and regression
  risk.
- **Regression-first acceptance.** Existing held-out replay and self-test gates
  are the right place to reject plausible but unsafe rewrites.

Not directly transferable yet:

- **Full verifier-grounded clustering.** Deneb skill usage records currently
  have recent real-use errors, rejected edits, and replay cases, but not full
  execution traces with deterministic verifier outcomes for every skill call.
  The current implementation therefore labels causal status conservatively:
  "filtered real-use failure; trace-level causality unavailable."
- **Parallel candidate branches.** Deneb's evolver currently requests one
  candidate. Adding parallel branches would require a selector that evaluates
  multiple candidate bodies and merges compatible edits. That is a later change,
  not a prompt-only tweak.
- **Benchmark split equivalence.** Deneb is single-user production software, not
  Terminal-Bench. Held-out replay cases are the practical substitute for a large
  fixed benchmark split.

## Implemented in this pass

Runtime:

- `gateway-go/internal/domain/skills/genesis/evolver.go`
  now mines recent real-use failures into a "Self-Harness failure evidence
  bundle." The bundle clusters by:
  - terminal cause: timeout, missing artifact/path, permission/auth,
    schema/format, stalled loop, or other
  - causal status: real-use failure after existing review/infra filters
  - reusable agent mechanism: bounded execution, artifact recovery, preflight,
    structured contract, retry discipline, or uncategorized recurring failure
- The bundle is injected into candidate-generation, judge, and teacher-rewrite
  prompts.
- Candidate generation now receives structured cross-case evidence alongside
  rejected edits, optimizer memory, and held-out replay cases.
- Background-only evolution now requires repeated real failures with recent
  error evidence. A review finding can still trigger an evolve immediately, but
  the periodic underperformer loop no longer rewrites a skill after only one
  real failure or evidence-free failure counts.
- Accepted and rejected evolve attempts persist structured Self-Harness audit
  fields: target signature, edited surface, expected behavior change, and
  regression risk. `miniapp.skills.lifecycle` projects those fields so the
  native skill timeline can render or inspect them.

Prompts and skill docs:

- `evolveSystemPrompt` now requires candidates to target one supported,
  addressable failure mechanism or skip.
- `changes.description` must state target failure signature, edited surface,
  expected behavior change, and regression risk.
- `skillJudgeSystemPrompt` checks whether a candidate actually addresses the
  Self-Harness evidence bundle when present.
- `skills/CLAUDE.md`, `evolution-proposal`, and `skill-evolution` now describe
  the Self-Harness discipline explicitly.

Tests:

- Added clustering coverage for timeout/schema/missing-artifact style failures.
- Extended the evolver prompt test so validation cases and the failure evidence
  bundle are both visible to the LLM prompt.

## srv1 production feedback

Checked against srv1 on 2026-06-18:

- `deneb-gateway.service` was the real running process on port `18789`, with
  LMTP on `127.0.0.1:10024`.
- `skill_usage.jsonl` and `skill_genesis_log.jsonl` had live data, but
  `skill_rejected_edits.jsonl`, `skill_optimizer_memory.json`, and
  `skill_validation_cases.jsonl` were absent.
- `skill_genesis_log.jsonl` already contained accepted and rejected evolve
  history, including repeated `topsolar-db` and `email-analysis` attempts.

The high-ROI operational fix is therefore read-path recovery, not a one-off
migration script:

- `RecentRejectedSkillEdits` now merges `evolve_rejected` lifecycle entries as
  `lifecycle-fallback` records when the rejected-edit sidecar is absent or
  sparse.
- `OptimizerMemory` now derives accepted, rejected, and rolled-back directions
  from the lifecycle log when the optimizer sidecar has no signal.
- The first new optimizer memory write after a missing sidecar backfills
  existing lifecycle history before applying the current event, so production
  does not lose old lessons once the sidecar is recreated.
- Failed real-use skill turns now best-effort extract a held-out validation case
  from the session transcript with `Source=auto-failed-skill-use`; weak traces
  are skipped and duplicate session/skill cases are not re-appended.
- Evolution thrash is no longer only a health signal: the top thrashing skill is
  held out of background `SkillsNeedingEvolution` selection for a 24-hour
  cooldown, while non-thrashing skills can still evolve.

## Follow-up design

1. **Candidate branch selector.** Add a JSON schema that allows 2-4 candidate
   bodies, run deterministic preflight and self-test per branch, and commit only
   the best non-regressive candidate. Do not merge branches until conflict rules
   exist.
2. **Lifecycle UI surfacing.** The server now exposes audit fields; the native
   screen can add a compact "target / risk" row if the timeline proves useful.

## Operating rule

For self-improvement work, the default question is no longer "does this rewrite
look better?" It is:

> Which repeated, addressable failure mechanism does this change target, and
> which held-out signal proves it did not regress anything else?
