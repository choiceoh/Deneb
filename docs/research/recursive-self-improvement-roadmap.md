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
mappings independently flagged this phase. Components, all deterministic Go.

**Status (2026-07-11): every component below is LANDED.** P2 preconditions
from this phase are in place; the e-process runs in observation mode until
its disagreement labels justify cutover (P3 precondition #1).

- **Certificate ledger + evaluator version attribution** (SEA 2607.00871,
  RQGM 2606.26294): every accept/reject/rollback lifecycle event carries the
  judge/evolve artifact versions (SHA of the meta artifacts), judge model,
  score pairs, and held-out margins — the substrate P2/P3 consume. Additive
  JSONL fields; time-sensitive (labels lost daily until it lands).
  *Landed: #3433.*
- **e-process accept/rollback testing** (PACE): anytime-valid sequential
  testing primitive replacing point-estimate deltas; also fixes the known
  bug that in-flight rollback watches evaporate on SIGUSR1 restarts.
  *Landed: #3434 (primitive + watch persistence), #3439 (observation-mode
  wiring — legacy threshold still owns firing; `baselineTest` disagreement
  labels accumulate in the lifecycle ledger as cutover evidence).*
- **Flip gates + held-out isolation** (AgentDevel): pass→fail regressions
  block promotion regardless of aggregate score movement.
  *Landed: #3445 (per-case flip gate ahead of the aggregate/min-delta
  gates; visible/blind pool isolation predates it).*
- **Rollback evidence persistence + anchor distillation** (CPE 2605.09315):
  a rollback records the rejected body + failure trace as validation cases
  so the same bad edit cannot be re-proposed; confirmed evolves distill
  frozen anchor cases (preservation gate separate from the acquisition gate).
  *Landed: #3435.*
- **Gate fuzz harness** (verifier fuzzing 2606.01066): fuzz the deterministic
  gates BEFORE optimization pressure learns their bugs; fixes the confirmed
  min-delta wedge (one unfixable assertion permanently rejects all
  candidates for a skill). *Landed: #3436.*
- **GRAO exemplar retrieval + verifier scoreboard** (TPGO 2604.20714,
  CoVerRL 2603.17775): cross-skill confirmed-evolve exemplars fed few-shot
  into evolve prompts; judgeAccuracy/falseAcceptRate/candidate-diversity on
  `/health`. *Landed: #3437.*
- **Reproduction oracle** (SEA Alg 8): at evolve commit, the producer also
  authors a case reproducing the fixed defect; adopted only after the
  deterministic gate confirms fails-on-original AND passes-on-candidate —
  dissolving the zero-validation-case cold start at the point evolves
  happen. *Landed: #3446 (with meta-artifact sidecar refresh so pristine
  materialized prompts follow compiled-default updates).*

### P2 — Slow loop (revised: deterministic bench primary, health delta advisory)

**Status (2026-07-11): scaffold + both epoch benches LANDED, propose-only.**
Weekly `MetaEvolutionTask` (#3448) alternates producer/evaluator epochs,
assembles evidence (7d scoreboard, low-yield levers, meta-experience ledger)
and writes gated proposals as `<artifact>.proposed` + ledger entries — never
touching live artifacts. Evaluator-epoch fitness = judge-degradation gold
pairs (#3449); producer-epoch fitness = shadow-replay flip bench (#3450).
First live cycle verified in production (2026-07-11 19:56 KST: producer
epoch proposed quantifying evolve rule 14). Remaining: accumulate real
weekly-cycle bench data, then flip gated proposals to auto-adoption.

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

### L4 — Source-level self-editing (operator-authorized 2026-07-12)

**Status: conditionally OPEN.** The operator lifted the blanket
source-self-edit prohibition: the loop may now target gateway source as a
DECLARED propose-only surface (`gateway-go/internal/domain/skills/genesis/surfaces/surfaces.go`), with the execution
contract *dev-worktree edit → full gates green → PR → CI green → land →
auto-deploy hot-swap* and no human approval step. Three exclusions survive
the opening: security CODEOWNERS paths, the prompt-cache invariants, and the
**acceptance machinery itself** (gates/benches/e-process/rollback watch/the
surface whitelist — forbidden at record time; a loop must never be able to
queue an edit to its own acceptor, the unanimous 2026H1 principle). Auto-apply
for source waits on a deploy-level rollback watch (post-hot-swap health
regression → auto-revert); until then the executor lane is a coding agent
consuming the self-correction queue.

### P4 — Skill+tool bundles (SkillSmith, adapted)

When an evolve attempt's failure analysis names a missing/broken tool
capability, emit a **paired** self-correction coding candidate referencing the
skill and the failure signature, and record the pairing in the lifecycle log.
The skill half stays auto-apply behind existing gates; the tool half is
propose-only through the existing 자가코딩 review lane — source code remains a
forbidden self-edit surface. "Atomic" here means atomically *proposed and
tracked*, not atomically applied.

### P5 — Compounding (beyond structural completion)

**Status: PROPOSED (2026-07-12).** P1–P4 built the machinery and the audit
layer now reports it honestly; the binding constraint has moved from *missing
components* to *compounding rate*. Measured throughput at proposal time:
3 evolves + 2 genesis per 7d, 1 meta cycle, 0 resolved rollback watches,
0 judge-miss labels, 0 L4 dispatches. RSI value compounds as
(improvement per cycle) × (cycle rate) × (evolvable surface) × (label
fidelity) — and every factor except fidelity is near its floor. The sober
baseline applies: most evolutionary-agent "improvement" collapses into a few
edit types (2605.20086), so raw cycle count alone does not compound either.
Each workstream below pairs a throughput lever with a fidelity guard.

Five workstreams, in priority order:

1. **Demand generation — curriculum lane.** The pyramid's base is
   signal-starved: one operator's organic usage yields ~3 underperformer
   signals a week, and L2/L3/L4 all inherit the famine (labels are a function
   of evolves). Extend the synthetic-but-honest pattern (workout /
   adversarial-coverage / judge-accuracy) from *repair* to *growth*: a
   curriculum task mines capability demand from the operator's actual
   environment (wiki/workfeed domain entities, failed or declined requests,
   skill-coverage gaps vs the business calendar), proposes NEW skills with
   validation cases authored FIRST (reproduction-oracle pattern, SEA Alg 8),
   and feeds genesis. Guard: curriculum-born skills carry a provenance flag so
   real-usage stats never conflate synthetic demand with operator demand
   (ground rule 3). Refs: DemoEvolve 2605.24539, SkillFlow 2604.17308,
   EvoAgentBench 2607.05202.

   *Slice-2 LANDED (EnvDigest wiring).* The curriculum producer now sees the
   operator's active work (recent feed-item titles) and wiki environment
   (active counterparty domains) via the `EnvDigest` closure — demand mining
   widens beyond tracker-local evidence to target real environment gaps. No
   new data assembly (existing `workFeedStore.List` + `wikiStore.
   ActiveCounterpartyDomains` primitives); genesis stays a leaf.

2. **Calibration campaign — run the #3461 knobs, bounded (operator lever,
   zero new code).** Weekly meta cadence gives the slow loop ~4 fitness
   points a month; the benches and e-process are starved by default cadence,
   not by design. For a bounded 4–6 week window:
   `DENEB_META_EVOLUTION_INTERVAL_DAYS=2`,
   `DENEB_JUDGE_ACCURACY_INTERVAL_HOURS=4`,
   `DENEB_SKILL_WATCH_MAX_AGE_DAYS=5`, `DENEB_META_BENCH_SCALE=2`; revert
   when per-epoch bench n≥10. One-change-per-window survives at any cadence
   (RQGM); the drift brake and meta rollback watch contain the added risk.
   Cheapest ~3–5x on loop frequency available.

3. **Proactive L4 supply — self-renovation, not just self-repair.** All
   live code-candidate sources are reactive (runtime errors, tool gaps,
   harness rejections): a healthy gateway produces nothing, so L4 idles
   exactly when a self-improving system should be renovating. Add
   deterministic, evidence-bearing proactive miners that file propose-only
   scope=code candidates through the existing review lane: codebase-health
   structural findings (e.g. the volatile-contract blast 1033 on domain/wiki
   that 2026-07-12's session diagnosed by hand), runtime-health standing
   weaknesses (latency #1), deadcode-audit deltas. Every candidate carries
   the bench finding ID + evidence so review stays deterministic. Refs:
   Meta-Harness 2603.28052, Agentic Harness Engineering 2604.25850, Bilevel
   Autoresearch 2603.23420 (outer loop reads the inner loop's own code and
   traces — ~5x in their setting).
   *First slice landed:* `scripts/audit/health-finding-miner.py` files
   codebase-health high-severity findings (cap 3/run) plus the weakest
   runtime-health dimension (<60, cap 1/run) as `health-finding:<finding-id>`
   candidates over the new `miniapp.self_improvement_coding.record` RPC;
   reopen semantics mirror `selfCorrectionReopenBlocked`. Decision record:
   scripts-side miner, NOT a gateway PeriodicTask — the inputs (git checkout +
   history, journald) live outside the serving process, and the tracker stays
   the queue's single writer via the RPC (rationale in the module docstring).
   deadcode-audit deltas remain follow-up.

4. **Recursion-surface widening — externalize more of the generative half
   (P1 pattern, one artifact per window).** L2 can only improve what is an
   artifact; today that is three prompts. Candidates in order:
   failure-analysis prompt, exemplar-retrieval policy (GRAO K/selection),
   curriculum prompt (from workstream 1), the dispatch prompt embedded in
   coding-dispatch.sh. The acceptance circuit stays frozen (unanimous 2026H1)
   — width grows only on the proposal side, and each new artifact ships with
   a compiled fallback + sidecar refresh (#3446) and enters the
   meta-experience ledger. Ref: PACE 2605.23019 (prompts AND control logic on
   the slow timescale).

5. **Outer-loop fitness — ground L2 in operator-visible utility.**
   Gate-passage rates are proxies; the top-level signal should be utility the
   operator experiences: feed-card accept/reject (#3456 already ledgers it),
   quality-bench trends, runtime-health composite. Wire these as ADVISORY
   evidence into meta epochs first; promotion to a gate only after label
   fidelity is proven — PACE's false-accept lesson applies one level up.
   Refs: Adaptive Auto-Harness 2606.01770 (gap decomposition); Escher-Loop
   2604.23472 as the caution that closed mutual refinement without external
   grounding drifts.

   **Status: first slice LANDED (feed-card advisory evidence).** The 7d
   feed-card verdict aggregate (adopted/rejected/reverted + adoptionRate)
   now flows into the meta-evidence block and the `MetaRevisionRecord`
   ledger as an ADVISORY snapshot — the producer's prose is grounded on
   operator-perceived utility while every deterministic gate stays
   untouched. The aggregate is a read-side view of the existing `Action`
   ledger field (zero new signal source, zero new dependency).

   **Second slice LANDED (runtime-health advisory evidence).** The 7d
   per-model agentlog aggregate (p95 agentMs, error/timeout/tool-error
   rates) flows into the same meta-evidence block via an injected
   `RuntimeHealth` closure — no new persistence (reads the existing
   `agentlog.Writer.AggregateByModel`), no leaf-package break. The latency
   signal ("latency #1" at proposal time) now grounds the producer's prose.

   **Third slice LANDED (codebase-health advisory evidence).** The accepted
   health-v2 baseline (overall score, weakest pillars, accepted finding count)
   flows into the meta-evidence block via a `QualityBench` closure reading the
   checked-in baseline JSON. No live Python bench run in-process (leaf-package
   boundary preserved); the baseline IS the contract surface. The producer
   now sees "codebase at 82.7, weakest: change-locality 55, responsibility
   52.6" as standing context.

   All three operator-visible signals (feed-card, runtime-health,
   codebase-health) now ground the meta producer's prose. A live delta-vs-
   baseline (current run − baseline) for codebase-health would need either a
   Python-in-process bench (forbidden by the leaf boundary) or a cached
   snapshot written by an external script — a follow-up if the accepted-state
   advisory proves useful.

**Graduation ladder.** Autonomy expands on track record, never on calendar.
Staged locks and the evidence that flips them (thresholds are proposals,
operator-tunable):

| Lock (today) | Evidence to graduate | Action |
|---|---|---|
| runtime-error source not dispatchable (#3491 staging) | first 3–5 mined candidates review clean (no hallucinated signatures) | allowlist flip in coding-dispatch.sh + rsi_status L4_SOURCES |
| ~~health-finding source not dispatchable~~ **GRADUATED 2026-07-12** | first batch (7) reviewed clean: finding IDs deterministic + independently reproduced at HEAD, no hallucinated signatures, remediation directions actionable with the safety contract embedded; wiki pair ([1] volatile-contract / [6] volatile-hub) dedups via the reproduce-at-HEAD precheck; two doctrine-tension candidates (runtime/server fan-out = composition root, toolctx co-change = designed leaf) rely on the dispatch bail-out clause | flipped: coding-dispatch.sh + rsi_status (py & Go) |
| e-process observation mode (#3439) | disagreement labels n≥20, legacy-agreement ≥90% | cutover: e-process owns rollback firing |
| L4 daily dispatch cap = 2 | N dispatches with 0 deploy-watch rollbacks and ≥50% land rate | raise DENEB_DISPATCH_DAILY_CAP |
| deploy-watch = binary rollback only | one rollback exercised end-to-end (real or fire-drill) | open the source auto-apply tier per the L4 note |
| ~~calibration knobs at defaults~~ **WINDOW OPEN 2026-07-12 → close by 2026-08-23** | operator opened the P5-2 window (meta=2d, judge-accuracy=4h, watch-max-age=5d, bench-scale=2, curriculum=24h) | drop-in `~/.config/systemd/user/deneb-gateway.service.d/rsi-calibration.conf`; revert = delete + daemon-reload + restart |

**Sobriety guards (what "beyond" must not become).** 2605.20086 (most
evolved "improvements" are a few edit types) and 2607.04277 (sustained RSI
needs introspection current LLMs lack) bound expectations. The
countermeasures stay load-bearing: honest usage-source separation,
deterministic acceptance, the drift brake, and the independent auditor
(rsi-loop-audit). A P5 workstream that cannot show its gain in held-out,
operator-grounded numbers is theater and gets cut.

**L5 note (the layer above L4).** The one prompt L1–L4 hold fixed is the
meta-governor (`metaEvolutionSystemPrompt`). Whether it should ever become the
fourth evolvable prompt — and why it stays frozen until P3 is not just built but
*calibrated* (currently 0 graded labels) — is worked out in
[rsi-l5-meta-governor-unfreeze.md](rsi-l5-meta-governor-unfreeze.md). Design-only;
recommendation is keep-frozen, feed the loop instead.

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
