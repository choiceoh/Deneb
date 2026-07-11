# Recursive Self-Improvement Roadmap (North Star, 2026-07)

> Operator directive 2026-07-11: the project's primary goal is **recursive
> self-improvement (RSI)** of the Deneb agent — not just evolving task skills,
> but making the improvement procedure itself an evolvable artifact. This note
> maps the three anchor papers onto the existing genesis architecture and lays
> out the phase plan. The 비서실장 identity and product surface are unchanged;
> this reorients development priority.

## Anchor papers (arXiv IDs verified 2026-07-11)

| Paper | arXiv | Core idea | What Deneb takes from it |
|---|---|---|---|
| MetaSkill-Evolve (2026-07-06) | [2607.05297](https://arxiv.org/abs/2607.05297) | The improvement pipeline itself is a **meta-skill**, evolved on a slow timescale while task skills evolve on a fast one (two-timescale). Pipeline decomposed into Analyzer/Retriever/Allocator/Proposer/Evolver. | The fast loop already exists (evolver + nudger + idle backstop). The slow loop does not: our improvement procedure is hardcoded Go. Externalize its LLM-facing parts and evolve them. |
| CoEvoSkills (2026-04-02) | [2604.01687](https://arxiv.org/abs/2604.01687) | Co-evolve the skill **generator** with a **surrogate verifier**, so verification quality grows with generation quality instead of staying a fixed bottleneck. | Our judge/validation stack is static. Post-hoc outcomes (rollbacks, post-evolve usage) are exactly the labels a verifier can learn from — the tracker already records them. |
| SkillSmith (2026-05-31) | [2606.01314](https://arxiv.org/abs/2606.01314) | Evolve skills and **tools together** as atomic bundles — skill-only evolution plateaus when the missing capability is a tool. | Tools are Go source = forbidden self-edit surface (`editable_surfaces.go`), by design. The bundle idea maps to pairing a skill evolve with a **self-correction coding candidate** (propose-only, operator-gated). |

## What already exists (do not rebuild)

The genesis subsystem (`gateway-go/internal/domain/skills/genesis/`, rules in
`docs/agent-rules/self-improvement.md`) already implements the fast loop and
most of the measurement substrate the papers assume:

- **Fast loop**: `EvolveUnderperformers` (6h periodic) + mid-session `Nudger`
  + heartbeat idle-review backstop (#3427/#3428) + K-candidate generation with
  judge selection (`evolver_judge_teacher.go`) + teacher rewrite escalation.
- **Gates**: deterministic admissibility (`evolver_skill_validation.go`, pure),
  independent judge, held-out selection margin, post-evolve rollback watch
  (`DefaultRollbackThreshold`), cross-skill regression sweep.
- **Fitness signals**: `EvolutionHealthSummary` (accept rate, thrash, staleness)
  on `/health`; lifecycle log; usage-source gate keeping review self-activity
  out of real-world stats; validation-case corpus + session backfill.
- **Self-correction funnel**: deterministic recurrence/cluster promotion into
  coding candidates (`tracker_recurrence_promotion.go`), heartbeat review lane,
  operator-gated apply.

## Phases

### P1 — Externalize the improvement procedure (prerequisite for recursion)

Move the LLM-facing components of the evolve pipeline out of Go constants into
versioned artifacts under the managed genesis dir (e.g. `meta/evolve-prompt.md`,
`meta/judge-prompt.md`, `meta/teacher-prompt.md`), loaded with a hardcoded
fallback. Reuse the existing skill backup/rollback machinery
(`backupSkillVersion`/`RollbackSkill` pattern) for meta artifacts. No behavior
change at rollout: artifacts start as byte-copies of today's constants.

- Invariant: thresholds/gates (`evolver_skill_validation.go`, rollback counts)
  stay in Go — the *deterministic* half of the pipeline is not self-editable.
  Only the *generative* half (prompts) becomes an artifact.

### P2 — Slow loop (MetaSkill-Evolve two-timescale)

A weekly autonomous task proposes ONE meta-artifact revision per cycle, judged
against aggregate fitness — `EvolutionHealthSummary` deltas over the window
(accept rate up, rollback/thrash down, staleness down) — with automatic revert
when the next window regresses (the same post-evolve watch pattern, applied at
the meta level). Cadence asymmetry is the point: fast loop 6h, slow loop 7d,
one change at a time so attribution stays possible.

### P3 — Verifier co-evolution (CoEvoSkills)

Close the loop on verification quality using labels we already collect:
a rollback after an accepted evolve = judge false-accept; strong post-rejection
real usage = false-reject. Feed these as few-shot exhibits into the judge
prompt artifact (P1) and auto-expand the validation-case corpus from the same
events (the backfill machinery exists). The judge improves on the same slow
cadence as P2, never mid-window.

### P4 — Skill+tool bundles (SkillSmith, adapted)

When an evolve attempt's failure analysis names a missing/broken tool
capability, emit a **paired** self-correction coding candidate referencing the
skill and the failure signature, and record the pairing in the lifecycle log.
The skill half stays auto-apply behind existing gates; the tool half is
propose-only through the existing 자가코딩 review lane — source code remains a
forbidden self-edit surface. "Atomic" here means atomically *proposed and
tracked*, not atomically applied.

## Ground rules carried over

1. Deterministic gates and safety surfaces are never self-editable (P1
   invariant; `editable_surfaces.go` precedence stands).
2. One meta-change per slow window; every meta-change carries its fitness
   baseline and auto-revert condition.
3. Review-fork self-activity stays out of real-usage stats (existing
   usage-source gate) — meta-fitness must read the same honest signals.
4. Everything lands behind the existing lifecycle log so Propus and the
   operator can audit what the loop did to itself.
