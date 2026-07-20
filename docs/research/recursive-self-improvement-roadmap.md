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
| RHI (2026-07-17) | [2607.15524](https://arxiv.org/abs/2607.15524) | **Trajectory-local self-comparison**: judge each harness revision only against its immediate predecessor (Θ(1)/iteration, no population), accumulate the pairwise preference history, and let the recurring-issue delta checklist steer the next revision. Gains come from task-specific context management (contracts), not longer reasoning. | Adopted as the dreamer-local slow loop over `wiki-dream-rules.md` (P2 note above): comparison ledger + weakness delta checklist + gated revision. RHI's ungated replacement is replaced with Deneb's contract gate + `.bak` + loss-streak rollback. Contracts-first finding informs subagent prompt design (return contracts over instructions). |

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
  labels accumulate in the lifecycle ledger as cutover evidence). Cutover
  mechanism landed 2026-07-12: `EProcessCutoverReadiness` scores the labels
  against the ladder thresholds (n≥20, agreement≥90%) on `rsi_status`
  (Go + py), and `DENEB_EPROCESS_OWNS_ROLLBACK=1` hands firing to the
  e-process — both verdicts stay recorded post-cutover, so labeling never
  stops and the flip is auditable/reversible.*
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

**Status (2026-07-12): scaffold + both epoch benches LANDED; bench-gated
AUTO-ADOPTION is the default.** Weekly `MetaEvolutionTask` (#3448) alternates
producer/evaluator epochs, assembles evidence (7d scoreboard, low-yield
levers, meta-experience ledger) and runs every proposal through the
deterministic gate chain (contract gate + epoch bench). Evaluator-epoch
fitness = judge-degradation gold pairs (#3449); producer-epoch fitness =
shadow-replay flip bench (#3450). A bench-cleared proposal auto-adopts
(operator mandate 2026-07-11; kill switch `DENEB_META_AUTO_ADOPT=0`), arms
the meta rollback watch, and surfaces a post-hoc revert veto on the feed
card; the drift self-brake (`evolution_drift.go`) freezes auto-adoption back
to propose-only on reward-hacking signals. First live cycle verified in
production (2026-07-11 19:56 KST: producer epoch proposed quantifying evolve
rule 14).

*RHI self-comparison lane LANDED (2026-07-20, arXiv 2607.15524 adoption).* A
second, dreamer-local slow loop over the FOURTH evolvable artifact —
`wiki-dream-rules.md`, the externalized synthesis rules override
`loadWikiSynthesisRules` always anticipated: each dream cycle's proposal
report is pairwise-judged against its immediate predecessor
(trajectory-local, one small LLM call), the verdict + fixed-vocabulary
weakness tags accumulate in `.dream-selfcompare.jsonl`, and once a weakness
recurs (RHI's delta-checklist signal) a weekly-capped revision pass rewrites
the rules. RHI's unconditional replacement is deliberately not adopted:
adoption passes a deterministic contract gate (load-bearing invariant lines
must survive — the gate is itself regression-coupled to the compiled default
rules in tests), keeps a `.bak`, and a post-revision loss streak (≥2
previous-wins, 0 current-wins in the first 3 comparisons) auto-restores it.
Production-only (`SetRulesEvolution`, fail-closed; kill switch
`DENEB_DREAM_RULES_EVOLVE=0`).

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

**Boldness instrumentation landed (2026-07-14, the L1.5-trap telemetry).**
Bilevel Autoresearch (2603.23420) measured parameter-level tweaks of a fixed
mechanism as a null result while structural mechanism change carried the
entire gain — and a non-regression gate stack systematically selects timid
edits, so the loop can drift into the null regime invisibly. Every proposal
is now classified `structural`/`parametric` by a deterministic classifier
(`meta_revision_class.go`: heading text + numbered-rule presence/order =
skeleton; rule REWORDING — the "quantify rule 14" archetype — is parametric;
≥0.35 body-line churn with an intact skeleton is a rewrite → structural).
The class rides the ledger (`MetaRevisionRecord.RevisionClass`), surfaces on
rsi_status L2 (구조형·파라미터형 + 연속 파라미터형 채택), and at ≥3
consecutive parametric ADOPTIONS the producer epoch's evidence block carries
an explicit structural-candidate nudge. ADVISORY throughout — no gate, bench,
or brake reads it; the same paper's counter-repetition idea also landed on
the fast loop (K-candidate orthogonal lens rotation, `candidateVariationNote`).

### P3 — Verifier co-evolution (CoEvoSkills; preconditions added)

Close the loop on verification quality using labels we already collect:
a rollback after an accepted evolve = judge false-accept; strong post-rejection
real usage = false-reject. Feed these as few-shot exhibits into the judge
prompt artifact (P1) and auto-expand the validation-case corpus from the same
events (the backfill machinery exists). The judge improves on the same slow
cadence as P2, never mid-window.

**First organic-label slice landed 2026-07-12:** `OrganicFalseAccepts` mines
the lifecycle ledger for baseline-CONFIRMED rollbacks (the e-process agreed
the failure rate rose — the deterministic filter satisfying precondition #1
below until full cutover), attributes each to the accepting judge-artifact
version via the provenance certificate (#3433), and feeds them into the
evaluator-epoch evidence alongside the synthetic misses — scoped to the
incumbent judge, surfaced on `rsi_status` L3 (실전 라벨). Real-usage labels now
flow; volume still depends on evolve throughput (P5-1/P5-2).

**Low-confidence operator-label slice landed 2026-07-13:** an accepted skill
evolve whose judge score margin is close to the admission boundary now emits a
work-feed verdict card. `개선 확정` and `되돌리기` are persisted as idempotent,
judge-version-attributed labels in `judge_accuracy_log.jsonl`; a rollback is
accepted only while the card's exact skill version is still live and the backup
restore succeeds. These labels feed both the L3 status surface and the next
evaluator epoch, adding real verdict density without inventing synthetic usage.

**Probe curriculum ladder landed 2026-07-13:** the judge-accuracy lane's
planted-defect corpus was saturating (14 runs × 0 misses at the drop tier —
a static probe corpus the judge has outgrown produces zero labels forever,
starving every evaluator epoch). The lane now escalates to in-place WEAKEN
probes (`imperative-weaken`: a hard-rule token diluted to a preference in
place; `scope-narrow`: a universal quantifier shrunk — nothing absent for a
diff to catch, the guarantee simply gone) once the incumbent judge posts
`judgeEscalationWindow` consecutive zero-miss drop-tier runs. The honesty
invariant is unchanged (enforcement/coverage loss by construction, never
debatable harm); a drop-tier miss re-locks the tier, and a judge revision
resets the curriculum via version scoping — a fresh judge re-earns tier 3.
Weaken classes stay OUT of the meta-judge promotion gate, same separation as
the drop tier.

**Order-swap consistency gate landed 2026-07-20:** the L1 judge's accepting
forward verdict must now survive an order-swap probe — the same judge re-grades
the pair with the bodies in swapped prompt slots and must REJECT that reversed
pair; blessing both directions means the verdict tracked slot position, not
content (fail-closed on inconsistency and on probe errors; kill switch
`DENEB_JUDGE_SWAP_CHECK=0`). The outcome is attributed on the provenance
certificate (`judgeSwapConsistent`), so per-judge-version position-bias rates
accumulate as another organic L3 label stream requiring no gold answers.
Adapted from the 2026-07 literature sweep: pairwise contrastive validation
(arXiv:2607.14408) and the both-orders agreement audit protocol (Double
Ratchet, arXiv:2607.12790). Deferred from the same papers: accept-side gold
pairs for the evaluator-epoch bench mined from 개선 확정 operator labels
(Double Ratchet's validity gate would close the all-reject blind spot the
degradation-only bench leaves open), and pairwise Soft-Elo parent selection
(pointless before evolve throughput improves — P5-1). SPyCE
(arXiv:2607.13854) was reviewed for its skill-policy co-evolution loop and
adds no gate we lack; its explicit lack of drift protection is the failure
mode the charter/e-process/drift-brake stack already guards against.

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
The skill half stays auto-apply behind existing gates; the tool half is routed
through the L4 자가코딩 lane and its durable dispatch/result ledger. It can land
only through the declared source surface, full PR/CI/deploy gates, and the
deploy rollback watch. "Atomic" here means atomically *proposed and tracked*,
not atomically applied.

### P5 — Compounding (beyond structural completion)

**Status: ACTIVE.** P1–P4 built the machinery and the audit layer reports it
from canonical ledgers; the binding constraint has moved from *missing
components* to *compounding rate*. The 2026-07-12 proposal baseline was
3 evolves + 2 genesis per 7d, 1 meta cycle, 0 resolved rollback watches,
0 judge-miss labels, and 0 L4 dispatches. That sentence is historical evidence,
not current status. RSI value compounds as
(improvement per cycle) × (cycle rate) × (evolvable surface) × (label
fidelity) — and every factor except fidelity is near its floor. The sober
baseline applies: most evolutionary-agent "improvement" collapses into a few
edit types (2605.20086), so raw cycle count alone does not compound either.
Each workstream below pairs a throughput lever with a fidelity guard.

**Live-status contract (drift guard).** This roadmap stores architecture,
invariants, thresholds, and dated historical baselines only. Current counters
must be generated from the append-only ledgers:

```bash
python3 scripts/audit/rsi-status.py --markdown
python3 scripts/audit/rsi-status.py --write-markdown ~/.deneb/data/rsi-current-status.md
```

The formatter obtains the canonical Go snapshot over `miniapp.rsi.status`; it
does not re-read ledgers or duplicate layer policy. The generated document
includes its timestamp and gateway URL and is never hand-edited or checked in
as an allegedly current snapshot.

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
   **SkillCorpus (2607.15557, reviewed 2026-07-20)** adds two external
   anchors: (a) its 96K-skill study found composite LLM quality scores DO NOT
   predict per-task success (all |r|<0.10) — corroborating the deterministic
   replay-first acceptance principle; never rank or cull skills by judge
   score alone. (b) Per-task gain tracks retrieval-match quality (r≈0.31–0.40;
   +2.2pp lowest bin → +25.1pp highest) — a low best-match relevance score
   over real sessions is therefore a coverage-gap DEMAND signal; candidate
   future curriculum input once per-session match scores are ledgered
   (deferred: needs session-side scoring + ledger plumbing). Rejected from
   the same paper: corpus import (supply-chain surface + English-dominant),
   semantic dedup tiers (scale mismatch at our catalog size), and regex
   safety hard-gates (the paper itself measured >90% false positives,
   audit-only).

   *Slice-2 LANDED (EnvDigest wiring).* The curriculum producer now sees the
   operator's active work (recent feed-item titles) and wiki environment
   (active counterparty domains) via the `EnvDigest` closure — demand mining
   widens beyond tracker-local evidence to target real environment gaps. No
   new data assembly (existing `workFeedStore.List` + `wikiStore.
   ActiveCounterpartyDomains` primitives); genesis stays a leaf.

   *Slice-3 LANDED (calendar coverage-gap).* The digest — now owned by
   `runtime/curriculumenv` (extracted from the server composition root, #3574)
   — also surfaces upcoming business-calendar commitments (next 14d, titled
   events only, read from the process-wide `localcal` store). This is the
   forward-looking half of the demand signal ("skill-coverage gaps vs the
   business calendar"): the producer infers which capabilities imminent
   commitments will need, and the 12-rune verbatim-quote grounding gate keeps a
   proposal tied to a real event summary. Landing it in `curriculumenv` (not
   `server`) kept the change off the overloaded composition root.

   *Slice-4 LANDED (failed-request mining) — the demand-source set named by
   this workstream is now complete.* `agentlog.FailedUserRequests` joins
   `run.error` entries to their run's user message within REAL client
   sessions (`client:*`; the live-test synthetic `client:lt-*` prefix and
   `system:*`/`cron:*` sessions are excluded at the source — ground rule 3),
   dedups retries, and the digest renders each as a QUOTED request + error
   head over a 14d window (failures are scarce at single-operator cadence) —
   the strongest demand evidence available, since the environment already
   asked in its own words and the verbatim-quote gate can bind a proposal to
   the exact ask. Deliberate scope cut: agent-DECLINED requests ("that
   capability doesn't exist") are not deterministically detectable from a
   normally-completed run and would need LLM inference over transcripts —
   excluded to keep the lane deterministic; revisit only with a labeled
   decline signal.

   *Slice-5 LANDED (high-effort-run mining — SearchOS host-miner pattern,
   arXiv 2607.15257, 2026-07-20 review).* The implicit half of the demand
   signal: `agentlog.HighEffortUserRuns` selects REAL client runs that ended
   cleanly (end_turn, non-proactive) yet ground through ≥8 tool calls —
   the agent managed, but the hard way — and the digest renders each as a
   QUOTED request + effort stats (tool calls/turns, top-tool histogram,
   skills-consult marker), heaviest-first over a 14d window. Deterministic
   selection, LLM triage: the existing curriculum honesty gates (skip-first,
   12-rune verbatim quote, dedup window) decide whether a recurring grind
   shape warrants a skill, mirroring SearchOS's "opened repeatedly with poor
   generic yield → purpose-built skill" triage. The Slice-4 decline scope
   cut stands — this mines effort, not refusals.

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
   *Second slice landed:* `scripts/audit/deadcode-finding-miner.py` mines
   `deadcode-audit.sh` deltas (NEW dead code not in the checked-in baseline)
   into `deadcode-finding:<hash>` propose-only candidates (cap 3/run),
   importing the RPC edge + reopen/cap semantics from the health miner so the
   two cannot drift; a `workflow_dispatch` dry-run job on the srv4 runner
   (`.github/workflows/deadcode-finding-dryrun.yml`) exercises it on srv4.
   The `deadcode-finding` source stays OUT of the coding-dispatch allowlist
   (staged for review — the graduation flip is separate).

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

   *Candidate audit (2026-07-13):* the "failure-analysis prompt" and "teacher
   prompt" turn out not to exist as separate surfaces — failure analysis is
   deterministic Go formatting inside the evolve prompt, and the teacher
   rewrite already loads the SAME externalized evolve artifact
   (`evolver_judge_teacher.go` → `MetaEvolveSystemPrompt`). The real
   externalization candidates are the dispatch prompt (below), the curriculum
   prompt (kept compiled on purpose — the demand lane's honesty bar must not
   be self-relaxable; revisit only with an operator decision), and the
   retrieval policy (not a prompt; needs its own design).

   *Slice-1 LANDED (dispatch-contract externalization).* The POLICY half of
   the L4 coding-dispatch prompt is now the fifth meta artifact
   (`meta/dispatch-contract-prompt.md`, compiled default in
   `generation/prompts.go`, materialized + sidecar-refreshed by the gateway
   like the other four). The consumer is OUTSIDE the gateway process —
   `scripts/dev/dispatch_prompt.py` composes candidate data + contract text
   and stamps `promptVersion` (sha12) provenance into the dispatch marker
   (P1.5 attribution extended to L4). Honesty details: no second copy of the
   contract exists anywhere (an unusable artifact DEFERS the dispatch, never
   silently swaps prompts; a scripts-side test pins the artifact name against
   the Go constant), and rollout is behavior-neutral (byte-identical prompt
   when the artifact equals the compiled default — verified). NOT in the L2
   rotation: adoption of a revision stays operator-side until a
   dispatch-outcome bench exists.

   *Slice-2 LANDED (genesis epoch — the rotation actually widens).* The
   genesis system prompt is now the THIRD artifact the slow loop can revise:
   epochs rotate producer → evaluator → genesis, and a genesis-epoch
   proposal is gated by a new deterministic shadow bench
   (`meta_genesis_bench.go`): three COMPILED session fixtures (skill-worthy
   multi-tool workflows with user corrections, byte-identical inputs for
   both prompts, mirroring `Service.Generate`'s exact prompt shape) are
   replayed through incumbent and proposal on the PRODUCTION genesis model
   (`generation.Service.ShadowGenerate`, server-injected closure), and both
   outputs are scored by the production admissibility gate
   (`generation.BenchAdmissibility` — parse + specificity issues, LLM-free
   ground truth). A proposal that flips a scenario the incumbent handles
   cleanly (vague output, or skipping a known skill-worthy session) is
   rejected; a mean gate-issue regression beyond noise rejects; the contract
   gate pins the genesis response-schema anchors. Conservative first
   posture: a 0→0 issue margin reads low-confidence, so early genesis
   revisions route to the operator-verdict card rather than auto-adopting —
   auto-adoption begins only when a revision measurably fixes gate issues
   the incumbent produces. Evidence block adds the 30d genesis lane volume
   with an explicit "skip 규칙은 정밀화 방향으로만" honesty instruction.

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

> **Ladder-readiness engine landed 2026-07-13** (`genesis/rsi_ladder.go`; exposed
> through `miniapp.rsi.status`): every machine-checkable row below is now scored
> CONTINUOUSLY against its evidence stream (e-process labels, dispatch-outcome
> land rate, staged-source candidate counts, per-epoch bench samples since the
> P5-2 window opened) and surfaced as a fifth "졸업 사다리" card (Key `GRAD`)
> on `rsi_status` both sides — READY means "evidence met, operator decision
> available", and the engine NEVER flips a lock itself. The card never counts
> toward the L1–L4 "turning" headline. Non-ledgered evidence (deploy-watch
> rollback drill) stays an explicit MANUAL row rather than a silent claim.
> **Transition watch landed the same day** (`ladder_watch.go`, prod-gated 6h
> task): a row's transition INTO READY files a work-feed card once (snapshot
> persisted; a fall-back-and-re-earn fires again — a new decision moment), so
> scarce operator attention arrives exactly when a graduation decision
> becomes available instead of requiring dashboard polling (ANCHOR pattern).
>
> **UNLOCK EXECUTION DELEGATED 2026-07-14** (operator directive: "잠금
> 해제도 에이전트에게 맡겨버려. 그래야 재귀적 자기개선이지"). The watch now
> EXECUTES evidence-met unlocks itself via the loop-owned graduation state
> (`graduation_state.go` → `~/.deneb/data/graduation_state.json`, consumed by
> the Go admission/status paths and the shell executor's daily-cap check).
> Source admission has one Go owner; the Python status formatter consumes the
> RPC snapshot instead of reading this file. Trust architecture mirrors P2
> auto-adoption:
> compiled thresholds ARE the ratified policy (the loop executes, never
> edits — `rsi_ladder.go`/`ladder_watch.go`/`graduation_state.go`/
> `tracker_eprocess_cutover.go` joined the forbidden acceptance-machinery
> surface), kill switch `DENEB_AUTO_GRADUATE=0`, the drift self-brake pauses
> execution, every unlock/relock is lifecycle-ledgered, and the feed card is
> a notification with a 재잠금 veto. Auto-executable rows and their compiled
> evidence: e-process cutover (n≥20 ∧ agreement≥90%; env knob overrides both
> ways), dispatch cap 2→4 (decided≥5 ∧ land rate≥50% ∧ 0 ledgered
> deploy-watch rollbacks — `deploy-watch.sh` now appends a rollback ledger),
> staged-source admission (review-lane endorsements: accepted≥2 ∧ rejected=0
> per source — a rejection is a standing veto). The calibration-window and
> manual-drill rows stay notify-only (their flips live outside the process).

| Lock (today) | Evidence to graduate | Action |
|---|---|---|
| ~~runtime-error source not dispatchable~~ **GRADUATED 2026-07-15** (allowlisted in `rsiDispatchSources`; roadmap row was stale until 2026-07-19) | first-batch human review dropped 2026-07-15; the real bottleneck turned out to be SUPPLY — the 12h fold cadence lost hot-swap-wiped ring bursts (live 2026-07-19: a ~120-line embed-failure burst left zero trace), fixed by splitting fold (1h) from mining (12h) cadence | done; supply watch = `runtime_error_signature_state.json` signature counts vs candidate emissions |
| ~~health-finding source not dispatchable~~ **GRADUATED 2026-07-12** | first batch (7) reviewed clean: finding IDs deterministic + independently reproduced at HEAD, no hallucinated signatures, remediation directions actionable with the safety contract embedded; wiki pair ([1] volatile-contract / [6] volatile-hub) dedups via the reproduce-at-HEAD precheck; two doctrine-tension candidates (runtime/server fan-out = composition root, toolport co-change = designed leaf) rely on the dispatch bail-out clause | flipped: coding-dispatch.sh + rsi_status (py & Go) |
| e-process observation mode (#3439) | disagreement labels n≥20, legacy-agreement ≥90% — readiness now computed live (`EProcessCutoverReadiness`, rsi_status L1) | cutover mechanism landed 2026-07-12: set `DENEB_EPROCESS_OWNS_ROLLBACK=1` when readiness reads ready |
| L4 daily dispatch cap = 2 | N dispatches with 0 deploy-watch rollbacks and ≥50% land rate — *land rate is now MEASURED (2026-07-13): each dispatch marker records its session outcome (landed/declined/failed/timeout/attempted; `dispatch_outcome.py` decision table from PR-state + worktree facts, attempted reprobed on later ticks), aggregated on `rsi_status` L4 both sides* | raise DENEB_DISPATCH_DAILY_CAP |
| deploy-watch = binary rollback only | one rollback exercised end-to-end (real or fire-drill) | open the source auto-apply tier per the L4 note |
| ~~calibration knobs at defaults~~ **WINDOW OPEN 2026-07-12 → close by 2026-08-23** | operator opened the P5-2 window (meta=2d→1d on 07-18, judge-accuracy=4h, watch-max-age=5d, bench-scale=2, curriculum=24h) | drop-in `~/.config/systemd/user/deneb-gateway.service.d/rsi-calibration.conf`; **close is automated (2026-07-19): `deneb-calibration-harvest.timer` fires 08-23 → `scripts/audit/calibration_harvest.py --revert` harvests per-epoch bench evidence into a report+wiki page, deletes the drop-in, reloads the gateway** — the campaign can no longer end silently. Bench-supply fix (2026-07-19): skip cycles previously left NO bench sample (live: 5 benched cycles in week 1 vs target 30); `DENEB_META_BENCH_ON_SKIP=1` (set in the drop-in, dies with the window) benches the incumbent alone on skip — a pure bench-noise sample, never a gate input |

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
*calibrated* (as demonstrated by the generated L3 status and its label-quality
thresholds) — is worked out in
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
- Post-sweep addendum (2026-07-12, papers the sweep missed or that appeared
  after its cutoff): `rsi-research-2026h2-addendum.md` — 6 deep mappings
  (Blind Curator labeler blind-spot, RSEA heartbeat auto-apply readiness,
  memory-consolidation immunity validation, evaluator-collapse segmentation,
  ToE exemplar structuring, ANCHOR low-confidence verdict routing) + a ranked
  next-commit list.
