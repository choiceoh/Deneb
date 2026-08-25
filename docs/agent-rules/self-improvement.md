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
3. **Skill generation** → the model generates `SKILL.md`; `Persist` writes to the managed dir. `ErrSkillDeduped` (`generation/service.go:55`) is an intentional skip, not a failure.
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
| `judge_accuracy.go` | P3 planted-defect + false-reject lane. When the highest probe rung (reorder) saturates, `Run` thins to one pair per class (canary) instead of replaying the full catalog; a miss re-opens the corpus. |

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

## Direct-Memory Grammar (data, not code)

정본 사실을 사용자 발화에서 유도하는 **축 카탈로그는 데이터**다:
`gateway-go/internal/domain/memory/direct_grammar.json` (go:embed, 로더/검증은
`direct_grammar.go`). 축 하나 = 안정 fact key 하나 + 그 축에 묶이는 한국어/영어 표현
(classify 패턴, 영어 assert/forward 명령, 삭제 명령의 목적어·큐).

- **왜 데이터인가**: 이 카탈로그가 정본 기억의 유일하게 좁은 입구다. 못 잡는 표현은
  곧 조용히 잊히는 사실이므로 계속 자라야 하고, JSON 한 파일이면 개선 루프가 직접
  넓힐 수 있다. 반면 **구조적 가드는 Go에 남는다** — 전문(傳聞)·임시 스코프·따옴표
  페이로드·제3자 주어는 축별 규칙이 아니고, 데이터 수정으로 약화돼선 안 된다.
- **축 추가 절차**: JSON에 축을 추가 → `go test ./internal/domain/memory/`
  (카탈로그 검증 + 축별 바인딩 테스트) → 필요하면 `make fact-bench`로 라이프사이클
  영향 확인. 새 축은 `key`(네임스페이스), `kind`(preference|identity), `classify`
  패턴이 필수다.
- **미탐 캡처**: 명령 형태(기억해/앞으로/정정/remember/from now on/forget)인데 어떤
  축에도 안 묶인 신뢰된 직접 발화는 `~/.deneb/data/memory_grammar_misses.jsonl`에
  기록된다 (`DirectMemoryMissFor` → `RecordDirectMemoryMiss`, 상한 1MB). 순수
  진단이며 그 턴의 동작에는 영향이 없다 — 카탈로그를 넓힐 근거를 모으는 용도다.
  권위 경계는 [ADR-0005](../adr/0005-fact-write-authority.md).
- **미탐 읽기**: `make memory-grammar-misses` (`scripts/dev/memory-grammar-misses.py`)가
  장부를 명령 lead·값을 지운 "형태"로 묶어 빈도순으로 보여주고, 각 묶음에 이미 가까운
  축이 있으면 그 키를 짚어 준다. 가까운 축이 있으면 그 축의 표현을 넓히는 일이고,
  없으면 새 축이 필요한지부터 판단할 일이다. 읽기 전용이라 장부를 소비(삭제)하지 않는다.
- **검색 어휘**: 축의 `queryAliases`는 그 사실을 찾을 때 사용자가 실제로 쓰는 말이다.
  정본 키는 영어인데 질의는 한국어라, 이 목록이 없으면 위키 fact 검색이 축 사실에
  아예 닿지 못한다(도입 전 한국어 질의 적중률 0). wiki 검색과 채팅 회상이 같은
  `memory.FactKeyQueryAliases`를 쓰므로 축에 표현을 더하면 두 표면이 함께 넓어진다.
  `make fact-bench`가 현행 값 도달 가능성을 게이트로 확인한다.

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
  result as `verified`, `no_effect`, or `regressed`. Miner-authored sources
  (`health-finding`, `tool-quality`, `runtime-error`, `deadcode-finding`)
  without a named contract stay in the queue as 검토 대기 — they do not
  auto-dispatch (2026-08-16; the unchecked-landing pile was 21 health-finding
  and 7 tool-quality). Reactive sources (`evolve-tool-gap`, `self-harness`) stay
  exempt. Legacy rows without a contract remain valid and do not fabricate an
  impact verdict. The health
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
8. **One unusable replay case must not blind a skill's behavioral gate.**
   `runBehaviorGate` scores the original twice and the candidate once on the
   SAME case set — that is what makes the delta a signal rather than noise — so
   a case any of the three cannot execute is dropped from **all three** and the
   remainder still scores. It used to abort the whole gate: the executor
   answered "실제 YouTube URL이나 영상 요청 없이 …" on a case whose input names no
   video, the plan parse failed, and that skill stopped being gate-checked
   entirely. Measured 2026-08-26 the lifecycle ledger read `evolved: 0` across
   every skill. Every executor failure now logs the **case** id, not just the
   skill — the failure is nearly always a property of one case, and without an
   id nobody can find which one to repair.

   The case that caused it was itself a **generator** defect: adversarial
   tool-coverage cases synthesized their replay input as `exercise the skill's
   use of tool <X>` — a meta-instruction, not a user task, so the executor
   refuses and the plan parse fails. The generator now **borrows a real task**
   from one of the skill's existing cases, and leaves `Input` empty when none
   exists: an empty input keeps the case deterministic-only (`RequiredTools` is
   still scored against the body) instead of pretending to be executable. 42
   legacy records across 18 skills carry the old placeholder and are filtered
   out of the behavioral set by prefix — the store is append-only so they stay,
   and they still serve the deterministic gate. The generator also used to treat
   parameter names (`max_results`, `no_reply`, `db_path`) as "tools" — its
   pattern was any snake_case token. It now matches against the **registry's own
   names**, injected as `KnownTools` because the tool registry lives in the chat
   pipeline and this is a domain package. With no registry wired it authors
   section coverage only: a coverage case built on a guess is worse than no
   coverage case.
9. **A bundled-skill deletion is a tombstone, and a tombstone hides itself.**
   The repo tree is a production checkout so the files cannot be removed;
   `miniapp.skills.delete` records the name in `~/.deneb/data/deleted_skills.json`
   and the catalog filters it out of **every** surface — including the list a
   restore would be reached from. Use `miniapp.skills.deleted` to see them and
   `miniapp.skills.restore` to undo. Before that undo existed this cost real
   capability twice: `kb-interview` swallowed two exact operator triggers on
   2026-08-18 (tombstoned 07-21, nothing said so), and on 2026-08-26 an RSI
   check found `evolution-proposal` and `skill-factory` — the loop's own
   proposal and skill-creation machinery — tombstoned without intent.
10. **A bench-cleared proposal the bench cannot RANK goes to a tie-break judge,
   not to a person.** Low-confidence routing (margin ≤ 0, "no measurable
   improvement") used to request an operator verdict; measured 2026-08-26 that
   ask went unanswered — one proposal expired after three weeks pending. The
   judge decides only those ties: it cannot accept what the bench rejected or
   reject what the bench auto-adopts, so the deterministic chain is still the
   approver. A missing judge, a call error, or a verdict with no rationale all
   fall back to the operator card — a broken judge stalls the loop instead of
   adopting on a guess.
11. **A vocabulary the code shares must be DERIVED, never written down twice.**
   Three defects found on 2026-08-26 were one defect: a component needed to know
   "what tools exist" and had nowhere to ask, so it substituted whatever was in
   front of it — the coverage generator took any snake_case token in a SKILL
   body, the evolve judge took the incumbent SKILL.md (rejecting every repair of
   a stale skill as fabrication), and a skill body took a real name from a
   NEIGHBOURING vocabulary (`deneb.deal_ledger` for the tool `deal_ledger`,
   whose bridge attribute is `deals`).
   Authority differs per vocabulary and each consumer must take the right one:
   the complete tool set is **runtime-only** (`Handler.ToolNames()` — MCP servers
   register dynamically, so no checked-in list can be complete), while the
   code_action bridge surface is a closed list **derived from the embedded
   runtime** (`codeaction.BridgeSurface()` parses the class that defines the
   methods). Both are injected, and a consumer with no authority wired must
   DEGRADE, not guess — author nothing, omit the section, skip the check —
   because a guess reads as an answer.
   The copy is the failure mode, not the absence of a list. The first version of
   the bridge guard hardcoded eight names with a "keep in sync" comment and had
   already missed `gmail` **on the day it was written** — the exact drift it
   existed to catch, committed by the guard itself.
12. **A lane that goes quiet must say so — silence is not evidence of health.**
   Deneb is built to never break a user turn (~326 `silently`, ~295
   `best-effort`, ~104 `advisory`, ~50 `fail-open` in the tree). That judgement
   is right and it has one cost: a broken subsystem is indistinguishable from an
   idle one — no error, `/health` green, nothing happens. Every finding in the
   2026-08-26 runtime review was that cost coming due (map reader alive 10 days
   after its client died · `kb-interview` tombstoned five weeks while the
   operator typed its exact triggers · `evolved: 0` behind a disabled gate · a
   proposal pending from 08-03 until it expired).
   `runtime/lanewatch` asks each watched lane whether it produced work and
   reports prolonged silence. The hard part is **not crying wolf**: a lane
   declares its own silence budget AND whether a zero is expected right now
   (`Reading.Idle`), because a watch that fires on healthy idleness gets muted
   and is then worth nothing. Adding a lane is a name, a budget, and a read —
   grow the set as more silent failures are found. New autonomous lanes should
   register one when they land, not after their first outage.
13. **Impact never rewrites delivery truth** — an ineffective or regressed
   result does not erase `watch_passed`/`applied`; it is independent evidence
   for prioritization and later policy, and can only be recorded for the same
   attempt after the safety watch passes.

## Where to Look Next

- `skills/CLAUDE.md` — discovery precedence (bundled < managed), workspace overrides
- `tracker_optimizer_memory.go` — slow-memory injected into the evolve prompt
- `tracker_rejected_edits.go` — rejected-edit buffer fed back into evolve prompts
- server-side capture wiring (`promoteRecurrences`, heartbeat self-improve sweep)
  lives in `runtime/server`, outside this package
