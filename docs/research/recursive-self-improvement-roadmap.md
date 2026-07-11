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

- **Fast loop**: `EvolveUnderperformers` (6h periodic) + mid-session `Nudger` +
  heartbeat idle-review backstop (#3427/#3428) + K-candidate generation with
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

### P1 — Externalize the improvement procedure (LANDED, #3430)

Move the LLM-facing components of the evolve pipeline out of Go constants into
versioned artifacts under the managed genesis dir (e.g. `meta/evolve-prompt.md`,
`meta/judge-prompt.md`, `meta/teacher-prompt.md`), loaded with a hardcoded
fallback. Reuse the existing skill backup/rollback machinery
(`backupSkillVersion`/`RollbackSkill` pattern) for meta artifacts. No behavior
change at rollout: artifacts start as byte-copies of today's constants.

- Invariant: thresholds/gates (`evolver_skill_validation.go`, rollback counts)
  stay in Go — the *deterministic* half of the pipeline is not self-editable.
  Only the *generative* half (prompts) becomes an artifact.

### P1.5 — Acceptor hardening (NEW, 2026H1 research revision)

The single strongest convergence of the 2026H1 literature sweep
(`rsi-research-2026h1.md`): self-evolution succeeds or fails on the
statistical trustworthiness of the ACCEPTANCE mechanism, not candidate
quality (PACE 2606.08106: greedy score-based acceptance yields 72-100%
false commits at the no-headroom regime; AgentDevel 2601.04620: removing
flip-gating raises regression rate 3.1%→14.8%). Eleven of eleven deep
mappings independently flagged this phase. Components, all deterministic Go:

- **Certificate ledger + evaluator version attribution** (SEA 2607.00871,
  RQGM 2606.26294): every accept/reject/rollback lifecycle event carries the
  judge/evolve artifact versions (SHA of the meta artifacts), judge model,
  score pairs, and held-out margins — the substrate P2/P3 consume. Additive
  JSONL fields; time-sensitive (labels lost daily until it lands).
- **e-process accept/rollback testing** (PACE): anytime-valid sequential
  testing primitive replacing point-estimate deltas; also fixes the known
  bug that in-flight rollback watches evaporate on SIGUSR1 restarts.
- **Flip gates + held-out isolation** (AgentDevel): pass→fail regressions
  block promotion regardless of aggregate score movement.
- **Rollback evidence persistence + anchor distillation** (CPE 2605.09315):
  a rollback records the rejected body + failure trace as validation cases
  so the same bad edit cannot be re-proposed; confirmed evolves distill
  frozen anchor cases (preservation gate separate from the acquisition gate).
- **Gate fuzz harness** (verifier fuzzing 2606.01066): fuzz the deterministic
  gates BEFORE optimization pressure learns their bugs; fixes the confirmed
  min-delta wedge (one unfixable assertion permanently rejects all
  candidates for a skill).
- **GRAO exemplar retrieval + verifier scoreboard** (TPGO 2604.20714,
  CoVerRL 2603.17775): cross-skill confirmed-evolve exemplars fed few-shot
  into evolve prompts; judgeAccuracy/falseAcceptRate/candidate-diversity on
  `/health`.

### P2 — Slow loop (revised: deterministic bench primary, health delta advisory)

A weekly autonomous task proposes ONE meta-artifact revision per cycle. The
original fitness design ("EvolutionHealthSummary deltas + auto-revert") was
independently rejected by six 2026H1 mappings (noisy point estimates,
memoryless repetition collapse, erosion blindness, flip-free regressions,
judge self-grading circularity, unobserved reward accuracy). Revised
promotion gate, in order:

1. **Frozen anchor bench preservation** (CPE) — no anchor regressions.
2. **Shadow-replay flip gate** (AgentDevel) — no pass→fail flips on the
   held-out corpus vs the incumbent artifact.
3. **Judge-degradation bench** (BabelJudge 2606.22329) — for judge-artifact
   revisions, a deterministic bench of controlled-degradation gold pairs is
   the only fitness; a judge must never grade its own revision.
4. `EvolutionHealthSummary` deltas — advisory only.

Meta-experience memory is mandatory (TPGO: memoryless meta-loops collapse,
30.0→14.5% in their ablation): each weekly cycle reads the ledger of prior
meta-revisions and their outcomes. Cadence asymmetry stands: fast 6h, slow
7d, one change per window, evaluator/producer epochs alternate (RQGM).

### P3 — Verifier co-evolution (CoEvoSkills; preconditions added)

Close the loop on verification quality using labels we already collect:
a rollback after an accepted evolve = judge false-accept; strong post-rejection
real usage = false-reject. Feed these as few-shot exhibits into the judge
prompt artifact (P1) and auto-expand the validation-case corpus from the same
events (the backfill machinery exists). The judge improves on the same slow
cadence as P2, never mid-window.

Label quality is triple-threatened (2026H1 sweep) — three preconditions:

1. **Baseline-aware rollback first** (PACE): the current baseline-blind
   3-in-6 rollback mislabels; fix before harvesting rollbacks as labels.
2. **Fuzz before co-evolving** (2606.01066): an exploitable gate poisons the
   very labels used to improve it (exploit passes → rollback → false-accept
   mislabel).
3. **Charter cases frozen** (SkillAudit 2606.14239): a held-out charter
   subset of validation cases is explicitly EXCLUDED from co-evolution — the
   structural hedge against false-accept drift. Consensus-trap guards
   (isolation + diversity monitoring, CoVerRL) apply to any majority-vote
   label augmentation.

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

## Research base

- Anchor papers: table above. Full 2026H1 sweep (118 verified papers, 14
  deep-mapped against this codebase): `rsi-research-2026h1.md` — includes the
  per-paper code-change mappings and the five next-commit candidates this
  revision draws from.
