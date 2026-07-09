# Instruction-Surface Evolve Design

> Design note for the last piece of the Self-Harness adoption program
> (2026-07). Status: **design only** — nothing here is implemented, and the
> declared-surface registry (`genesis/editable_surfaces.go`, #3315) keeps every
> surface below at `propose-only` until the gates in this document exist and
> the operator flips the tier explicitly.

## Why

Self-Harness (arXiv:2606.09498) showed the largest measured gains from edits to
*runtime instructions and policies* — bootstrap/verification instructions,
loop-breaker middleware, tool-error recovery prompts — not from per-task
content. Deneb's only auto-apply surface today is the SKILL.md body, which is
the *lowest-leverage* instruction surface: a skill is consulted only when a
turn loads it. The high-leverage surfaces (heartbeat contract, workspace
context files) are propose-only because they lack the one thing that makes
skill-body evolve safe: **a behavioral regression gate**.

The skill-body gate stack, for reference (all in `genesis/evolver.go`):

| Gate | Mechanism |
|---|---|
| patch-first budget | changed-ratio / section caps (coverage-conditional since #3313) |
| held-out replay | `SkillValidationEngine` replays real-session fixtures against original vs candidate |
| cross-model judge | paired scores, margin coverage-conditional |
| self-harness audit | target_signature must match a mined failure cluster |
| rollback watch | post-evolve failures auto-restore the backup |

Promotion rule for any new surface: build the *equivalent* of each row, or
declare explicitly which row is intentionally absent and why that is safe.

## Surface 1. HEARTBEAT.md (heartbeat turn contract)

The most promising candidate: bounded blast radius (one autonomous lane, no
user-facing chat), high edit frequency value (lane behavior tuning), and its
outcomes are already logged (heartbeat turns, sweep firings, NO_REPLY
discipline).

Gate design — shadow replay over recorded heartbeat contexts:

1. **Corpus**: persist the last N heartbeat turn inputs (assembled trigger +
   signal block, minus live side effects) as replay fixtures — the same
   harvest-don't-author principle as the validation backfill lane (#3312).
   Split held-in/held-out by time, like the paper.
2. **Executor**: run original vs candidate HEARTBEAT.md over each fixture with
   the lightweight role (same both sides so executor bias cancels — the
   existing replay-executor argument in `init_genesis.go`).
3. **Verifiers** (deterministic first): NO_REPLY discipline preserved on
   quiet fixtures; required actions still chosen on actionable fixtures
   (e.g. a sweep fixture must still yield `self_correction_propose`); output
   length/format budget. A paired judge is a *secondary* signal only.
4. **Acceptance**: the no-trade-off rule — held-in delta >= 0 AND held-out
   delta >= 0 AND at least one > 0, over verifier pass counts. Reject on any
   regression.
5. **Rollback**: keep the previous HEARTBEAT.md as a timestamped backup; K
   consecutive post-apply heartbeat anomalies (error logs, missed sweep
   firings vs signal state) auto-restore, mirroring the skill rollback watch.

Cost bound: fixtures are text-in/text-out lightweight-role calls;
2 x N calls per candidate with N around 10 is comparable to one skill evolve.

## Surface 2. Workspace context files (AGENTS.md, TOOLS.md, ...)

**Keep propose-only for now.** Two structural blockers:

- **Prompt-cache blast radius**: these files sit in the cached system prefix
  (`docs/agent-rules/prompt-cache.md`). Any edit invalidates the APC prefix
  for *every* session. That is acceptable for a rare, operator-reviewed
  change; it is not acceptable for an autonomous loop that may retry.
- **No fixture-shaped verifier**: their effect is diffuse (every turn,
  every task). The honest gate would be the live quality suite
  (`live-test.sh quality`) run against a shadow gateway — infrastructure that
  exists for operators but is not yet drivable headless from the evolve loop,
  and each run costs real LLM traffic on the DGX hosts.

Revisit when (a) a headless quality-suite runner exists and (b) edits can be
batched to amortize the cache invalidation. Until then the loop records
proposals with `surface: propose-only` and they land through reviewed PRs.

## Phasing

- **P0** (prereq, small): persist heartbeat turn fixtures (harvest lane,
  deterministic, mirrors `validation_backfill_task.go`).
- **P1**: shadow-replay executor + verifiers + no-trade-off gate, exposed as a
  dry-run report via `skill_lifecycle(action=heartbeat_shadow_replay)` —
  *still propose-only*; the report just accompanies the proposal so review is
  measured. (Implemented: `server/heartbeat_shadow_replay.go`.)
- **P2**: flip `heartbeat-instructions` to `auto-apply` behind an env flag
  (default off), with backup + rollback watch. Operator flips the registry
  tier after observing P1 reports for a while.
- **P3** (not scheduled): context files, pending the two blockers above.

## Open questions

- Heartbeat fixtures capture *signal state at fire time*; replaying them under
  a changed gateway version can drift from what the lane would really do.
  Mitigation: refresh the corpus continuously (rolling window), same as the
  validation backfill lane.
- Weak-evaluator risk (the paper's own top challenge): heartbeat "quality" is
  partly judgment. The design therefore weights deterministic verifiers over
  judge scores and keeps the operator in the promotion loop until P2 evidence
  accumulates.
