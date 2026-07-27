---
description: Self-improvement (genesis) loop — skill creation, evolution, curation, self-correction capture, and the gates that keep the loop honest
globs:
  - "gateway-go/internal/domain/skills/genesis/**"
---

# Self-Improvement (Genesis) Subsystem

Automatic skill creation and evolution from session experience — package
`internal/domain/skills/genesis`. The loop turns real usage/failure evidence into
new or rewritten skills, behind gates that keep it from thrashing or editing
things it shouldn't.

> Drafted by `scripts/audit/doc-draft.py` (glm-5.2, grounded in `go doc` + the
> call graph), then curated against source. Verify anchors before relying on them.

## Entry Points

| Entry | Caller | When |
|---|---|---|
| `LogGenesis` (`genesis/tracker.go`) | session pipeline | After a skill-worthy session — records `SkillActivityGenesis`, calls `markSkillAgentCreatedLocked` (`curator.go`) |
| `maybeFireEvolveLocked` (`genesis/tracker.go`) | via `LogGenesis` | Event-driven: every `DefaultEvolveEventThreshold` (3) new skills, min-gap respected |
| `EvolveUnderperformers` (`evolver.go`) | `autonomous.PeriodicTask` | Background cycle over underperforming skills |
| `EvolveSkill` (`evolver.go`) | skill-review fork / explicit | One skill, optionally with a `reviewFinding` directive |
| `Nudger` (`nudger.go`) | session runtime, mid-session | Every `DefaultNudgeInterval` (3) tool calls; strictly background |

## End-to-End Flow

1. **Session completes** → `LogGenesis` records the activity and marks the skill agent-created (`genesis/tracker.go` → `curator.go:markSkillAgentCreatedLocked`).
2. **Threshold check** → `maybeFireEvolveLocked` fires the evolve trigger at 3 new skills, respecting a min-gap.
3. **Skill generation** → the model generates `SKILL.md`; `Persist` writes to the managed dir. `ErrSkillDeduped` (`generation/service.go:54`) is an intentional skip, not a failure.
4. **Catalog registration** → discovered by precedence `bundled < managed` (see `skills/CLAUDE.md`).
5. **Evolution** → `Evolver.EvolveSkill` / `EvolveUnderperformers` rewrites; a candidate is judged by an independent judge model (`SetJudge`).
6. **Validation gates** → `evolver_skill_validation.go`: the self-harness audit must name a target failure signature, and `validateSelfHarnessEditedSurface` verifies the claimed `edited_surface` matches the section actually changed.
7. **Post-evolve watch** → the tracker watches next uses; `DefaultRollbackThreshold` (3) consecutive failures → `RollbackSkill` restores the backup.
8. **Curation** (`curator.go`) → staleness lifecycle `active → stale → archived` at `staleAfterDays` (30) / `archiveAfterDays` (90).

## RSI Lifecycle Boundaries

L1-L5 share a conceptual observe → evaluate → verify progression, but not a
generic runtime orchestrator. Each layer owns its producer, deterministic gates,
bench, cadence, and rollback policy. `genesis/lifecycle` stores only stable
L1-L4 display identity plus the authoritative L4 review/delivery state machine;
it does not declare unused automation, artifact, or frozen-L5 policy.

For L4, review and delivery are independent axes. `accepted` authorizes an
attempt; it does not mean the change shipped. Delivery advances through
`started` → `pr_opened` → `merged` → `deployed` → `watch_passed`, and only
`watch_passed` derives review state `applied`. `declined` is a healthy no-op;
`failed` and `rolled_back` are retryable after local residue checks.

## Key Files

| File | Role |
|---|---|
| `evolver.go` | `Evolver` — rewrite orchestrator (~844 LOC) |
| `genesis/tracker.go` | `Tracker` — activity log, lifecycle, event triggers, usage-source gate |
| `generation/service.go` | `Service`, `Config`, generation/persist orchestration |
| `curator.go` | Staleness classification + archive; `markSkillAgentCreatedLocked` |
| `nudger.go` | Mid-session background nudging (shares Service cooldown / `MaxSkillsPerDay`) |
| `evolver_adopt.go` | Copy-on-evolve **adoption** of bundled skills (`SetAdoptionDirs`) |
| `evolver_skill_validation.go` | Deterministic admissibility gates — **pure**, no `Evolver` state |
| `surfaces/surfaces.go` | `EditableSurface`, `ClassifySurface`, `DeclaredEditableSurfaces` whitelist |
| `workout.go` | Synthetic exercise lane — evidence only, never real usage |
| `curriculum.go` | Demand-generation lane (RSI P5-1) — coverage-gap mining; files route=genesis opportunities with validation cases authored first (source=`curriculum`, propose-only) |
| `tracker_self_correction.go` | Self-correction candidate record/query + forbidden-surface gate |
| `tracker_self_correction_dispatch_selection.go` | Canonical O(n) L4 candidate selection across review, delivery, source, and surface policy |
| `lifecycle/` | Stable L1-L4 display identity and authoritative L4 review/delivery state kernel |
| `tracker_recurrence_promotion.go` | Recurrence/cluster → self-correction candidate promotion |
| `failure_intervention_router.go` | Failure origin → cheapest intervention surface advisory routing (`shadow`; never changes dispatch or target policy) |
| `meta_evolution.go` | L2 slow loop — weekly meta-artifact revision (evolve/judge prompts) with epoch benches, auto-adopt + rollback watch |
| `runtime_error_mining.go` | L4 proactive source — recurring code-actionable errors → propose-only scope=code candidates |
| `retry_correction_miner.go` | Deterministic transcript mining of failed-then-successful tool retries into `tool_retry` evidence clusters for the sweep (EMG adoption, 2026-07-21) |
| `genesis/rsi_status.go` | RSI loop-status snapshot (`miniapp.rsi.status`) — L1–L4 layer state classification |

## Surface Tiers (`surfaces/surfaces.go`)

| Tier | Meaning |
|---|---|
| `auto-apply` | Loop may promote edits itself, behind gates |
| `propose-only` | Record a proposal only |
| `forbidden` | Never self-editable; rejected at record time so misaimed candidates never queue |

`DeclaredEditableSurfaces()` is ordered by precedence (first match wins); forbidden
entries lead so they cannot be shadowed. `classifyProposalSurfaces` applies this
gate when a candidate is recorded — source code is never a self-edit target.

## Usage Sources — the success-rate gate (`genesis/tracker.go`)

Only `real` usage feeds the evolver's success-rate gate. Conflating the review
fork's self-activity with real-world outcome drove the email-analysis evolve
thrash (`genesis/tracker.go:58`, PR #2328 — 6 evolves in ~2 days on one skill, undetected).

| Source | Feeds evolver gate? |
|---|---|
| `real` (client/cron turn) | ✅ Yes |
| `review-verdict`, `review-consult` (review fork) | ❌ No |
| `workout` (synthetic) | ❌ No |

## Self-Correction Capture Funnel

Distinct from skill evolution: the **self-correction candidate** queue
(`miniapp.self_improvement_coding.list`, stored `~/.deneb/data/self_correction_candidates.jsonl`)
is Deneb proposing fixes to itself. `RecordSelfCorrectionCandidate`
(`tracker_self_correction.go`) is the sink; states are proposed/accepted/rejected/
superseded/applied.

Deterministic promotion lives in `tracker_recurrence_promotion.go`:

- `PromoteTargetRecurrenceCandidates()` / `PromoteFailureClusterCandidates()` turn
  recurring failures (`FailureEvidenceClusters`, Support ≥ threshold) into
  candidates with no LLM in the loop — the reliable backstop when the LLM sweep
  ignores its nudge.
- Every `FailureEvidenceCluster` receives a deterministic two-axis shadow route:
  `failureOrigin` identifies where the contract first broke and
  `interventionSurface` names the cheapest likely repair surface. The route is
  visible in status, heartbeat sweep prompts, and promoted-candidate evidence,
  but is advisory only: it cannot change `TargetFiles`, editable-surface tier,
  review state, or dispatch eligibility.
- `selfCorrectionReopenBlocked` allows an **applied** candidate to reopen only
  after a cooldown once the same signature recurs (fixed-but-still-failing);
  **rejected** never reopens (operator veto is respected). Per-tick promotion is
  capped. (PR #3367; drain-side hardening PR #3380.)

## Meta-Evolution (L2) — the Slow Loop

Distinct from L1 skill evolution: `MetaEvolutionTask` (`meta_evolution.go`) is a
**weekly** task that revises the evolve/judge system prompts *themselves* — the
improvement procedure is an evolvable artifact. Producer and evaluator epochs
alternate (one change per window).

- **Evidence block** (`assembleEvidence`): the producer sees the 7d evolution
  scoreboard, low-yield levers, the meta-experience ledger (prior revisions +
  outcomes), and — for evaluator epochs — the live judge's labeled misses (P3).
- **Advisory grounding** (RSI P5-5): three operator-utility signals inform the
  producer's prose but **no gate reads them**:
  - feed-card accept/reject 7d (`OperatorUtilitySignals`, from the `Action` ledger)
  - runtime health — p95 latency, error/timeout rates (`RuntimeHealth` closure)
  - codebase health — overall score, weakest pillars (`QualityBench` closure)
- **Gates** (deterministic, inviolate): contract gate (`metaArtifactContracts`) →
  epoch-specific bench (judge-degradation gold pairs for evaluator, producer
  shadow-replay for producer) → auto-adopt with rollback watch. The drift
  self-brake (`evolution_drift.go`) freezes auto-adoption on reward-hacking
  trajectories.
- **Ledger**: `meta_evolution_log.jsonl` — every cycle records benches, adoption
  health, and the advisory snapshot for audit.

## Source Self-Edit (L4) — Proactive Code Candidates

The L4 lane files propose-only scope=code self-correction candidates targeting
gateway source. It is **conditionally open** (operator-authorized 2026-07-12):
the source surface is a declared propose-only editable surface, but the
acceptance machinery stays forbidden at record time.

- **Reactive sources** (built): `evolver_tool_gap.go` (evolve declares a missing
  tool), `runtime_error_mining.go` (recurring code-actionable error signatures).
- **Proactive sources** (scripts-side): `scripts/audit/health_finding_miner.py`
  mines codebase-health structural findings + runtime-health standing weaknesses;
  `scripts/audit/deadcode_finding_miner.py` mines `deadcode-audit.sh` deltas
  (newly-orphaned functions absent from the baseline);
  `scripts/audit/tool_quality_miner.py` mines per-tool error/malformed-arg-repair
  rates from the `miniapp.observe.behavior` aggregate and files the worst
  offenders as tool-description/schema clarification candidates (the tool
  descriptions are Go `ToolDef.Description` literals = the gateway-source
  surface; this grounds the previously-unconsumed agentlog quality signal);
  `scripts/audit/branch_rot_miner.py` mines the worktrunk fleet snapshot
  (`wt list --format json` on the dev checkout — docs/tools/worktrunk.md) for
  branches sitting ahead of main past a staleness bar with no open PR, splitting
  wt's trees-match detection into retire (already integrated, verify-then-delete)
  vs recover (rebase, then land or retire with rationale) candidates
  (deneb-branch-rot.timer, weekly). All
  file via the `miniapp.self_improvement_coding.record` RPC and share one
  RPC/reopen/cap edge (imported from the health miner so they cannot drift).
  Deliberately NOT gateway PeriodicTasks — the inputs (git checkout, journald,
  whole-program reachability, agentlog aggregate) live outside the serving
  process or are already exposed as a read-only RPC.
- **Dispatch ownership**: the gateway scans the current queue once and owns
  review/delivery/source/surface eligibility plus deterministic session-result
  classification. Safety-critical readers stream-fold the ledger and fail closed
  if any malformed or oversize row was skipped. `coding-dispatch.sh` only supplies
  local marker exclusions and execution facts; `dispatch_outcome.py` projects the
  authoritative phase onto a compatibility marker for worktree protection and
  older audits.
- **Safety and usefulness are separate axes**: `watch_passed` still means the
  merged deployment survived the rollback watch and derives review state
  `applied`. A candidate may additionally declare an `impactContract` (metric,
  increase/decrease direction, baseline, target, minimum samples, observation
  window, and named guardrails). After `watch_passed`, its impact is derived as
  `pending`; `miniapp.self_improvement_coding.impact` accepts observations for
  the exact dispatch attempt and deterministic Go classifies the terminal
  result as `verified`, `no_effect`, or `regressed`. Legacy candidates without
  a contract remain valid and do not fabricate an impact verdict. The health
  miner closes deterministic contracts from fresh reports. Supported metrics:
  `health.finding_present:<id>`, `runtime.health.score:<dimension>`,
  `health.score:overall`, `health.domain.score:<domain>`,
  `health.metric.score:<domain>/<metric>`, and the matching RSI Bench forms
  `rsi.bench.score:overall`, `rsi.bench.domain.score:<domain>`, and
  `rsi.bench.metric.score:<domain>/<metric>` (a globally unique metric id may
  omit the domain). Sibling miners close their own namespaces the same way
  (2026-07-27 — the ledger had 5 landed deadcode/tool-quality fixes nobody
  could distinguish from no-ops): `deadcode.finding_present:<id>` (deadcode
  miner, fresh audit run) and `tool.quality.finding_present:<tool>:<kind>`
  (tool-quality miner, fresh 7d recent window after a 7d observation window;
  below MIN_CALLS the verdict stays pending — silence is not success). All
  three share `pending_impact_observations_for` from the health miner so the
  lifecycle gating cannot drift. Unknown or unavailable metrics stay pending;
  ad-hoc evaluators use `self_correction_dispatch.py impact` rather than
  editing the append-only ledger. NOT contracted by design: the health miner's
  INCREMENTAL_KINDS (a bounded step leaves the finding correctly present) and
  branch-rot (the landing itself is the effect).
- **Impact drives retry policy**: within the same review state, dispatch ranks a
  source whose latest verdict is `regressed` before `no_effect`, then ordinary
  newest-first work. A latest `verified` verdict clears older negative priority.
  If the latest two terminal outcomes for one exact source are negative, another
  unattended attempt is blocked until `proposedChange` names a non-empty strategy
  different from every negatively measured strategy. L4 status uses the same
  complete-ledger policy and exposes blocked rows as `전략 변경 필요`.
- **Dispatch graduation (ladder)**: coding-dispatch.sh auto-dispatches only
  allowlisted source namespaces. **Graduated** (auto-dispatch → land through the
  full gate stack): `evolve-tool-gap`, `self-harness`, `health-finding`
  (2026-07-12, first batch reviewed clean), and `tool-quality` (2026-07-13, by
  direct operator directive ahead of a reviewed batch — narrow description/perf
  candidates, previewable via the tool-quality-dryrun workflow). **Staged** (file
  for review, no auto-dispatch): `runtime-error`, `deadcode-finding`,
  `sop-mining`, `branch-rot` (2026-07-20). **Graduation execution is DELEGATED to the loop (operator
  directive 2026-07-14)**: `LadderWatchTask` unlocks a staged source once the
  review lane endorses it (accepted≥2, rejected=0 — a rejection is a standing
  veto) by writing the shared graduation state
  (`~/.deneb/data/graduation_state.json`, read by Go admission/status and the
  shell daily-cap executor), with
  a lifecycle-ledger record and a feed-card 재잠금 veto. Kill switch
  `DENEB_AUTO_GRADUATE=0`; the drift self-brake pauses execution; the
  thresholds and the executor are forbidden self-edit surfaces (the loop
  exercises the policy, never edits it).

## Gotchas & Invariants

1. **Bundled skills are never seeded into the catalog.** The archiver would wipe
   rarely-used repo skills, and deploy rsyncs `skills/` with `--delete` (an
   in-place evolve edit is clobbered). The first evolve verdict on a bundled skill
   **adopts** it — copies the dir into the managed dir, which overrides bundled at
   discovery precedence. `SetAdoptionDirs(bundledDir, managedDir)` wires this;
   empty `bundledDir` disables adoption and catalog misses stay hard errors.
2. **`reviewFinding` lets evolve proceed with zero usage data** — `EvolveSkill`
   takes an optional directive from a background review; when present it is the
   primary basis for the rewrite.
3. **Nudger is strictly background** — never injects into the user-facing turn or
   mutates conversation messages, always fires via `pkg/safego` so a panic can't
   kill the process. `DENEB_SKILL_NUDGE_INTERVAL=0` disables.
4. **`RollbackSkill` is best-effort** — missing backup or catalog entry is a
   logged no-op, never a crash; mirrors the atomic write + lifecycle log so a
   revert propagates like an evolve.
5. **`EvolutionHealthSummary.Thrash`** surfaces silent thrash on `/health` — the
   loop burning its budget re-evolving one skill. Past silent deaths hid behind
   missing visibility; keep this fed.
6. **Validation gates are pure** — `evolver_skill_validation.go` holds no `Evolver`
   state, deliberately apart from orchestration in `evolver.go`.
7. **L4 ledger corruption freezes review and dispatch** — a skipped terminal
   review or delivery row could resurrect vetoed work, so safety decisions must
   surface malformed/oversize rows instead of silently using a partial fold.
8. **Impact never rewrites delivery truth** — an ineffective or regressed
   result does not erase `watch_passed`/`applied`; it is independent evidence
   for prioritization and later policy, and can only be recorded for the same
   attempt after the safety watch passes.

## Where to Look Next

- `skills/CLAUDE.md` — discovery precedence (bundled < managed), workspace overrides
- `tracker_optimizer_memory.go` — slow-memory injected into the evolve prompt
- `tracker_rejected_edits.go` — rejected-edit buffer fed back into evolve prompts
- server-side capture wiring (`promoteRecurrences`, heartbeat self-improve sweep)
  lives in `runtime/server`, outside this package
