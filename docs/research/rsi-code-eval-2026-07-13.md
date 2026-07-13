# RSI Code-Level Evaluation (2026-07-13)

> Code-level audit of the recursive self-improvement implementation against the
> claims in [recursive-self-improvement-roadmap.md](recursive-self-improvement-roadmap.md).
> Method: seven parallel deep-review passes (L1 fast loop, P1.5 acceptor, P2
> meta-evolution, P3 verifier co-evolution, L4 self-coding, audit/ladder layer,
> P5-1 demand lanes), full test-suite runs, and direct re-verification of every
> headline finding at HEAD (9bec273). This is a dated assessment, not a live
> status surface — current counters come from `scripts/audit/rsi-status.py`.

## Verdict in one paragraph

The architecture is real and the documentation is unusually honest: of ~40
roadmap "LANDED" claims checked, nearly all are CONFIRMED in code with the
mechanisms as described, a handful are PARTIAL, none are fabricated. The
deterministic-Go-decides / LLM-only-produces split genuinely holds, ground
rule 3 (synthetic vs real usage separation) holds at the single write path,
and both test suites pass (8/8 Go packages in `genesis/...`; 107 Python audit
tests). However, several flagship *safety* mechanisms are weaker than
documented at exactly the points that matter most: the e-process acceptor is
parameterized so it can never fire in production, the "loop cannot edit its
own acceptor" invariant is a convention rather than a gate against prose or
directory targets, one meta-evolution epoch can auto-adopt with no bench, and
a rollback leaves the in-memory catalog stale — corrupting the operator-label
chain built on top of it. The loop's *honesty instrumentation* is ahead of its
*enforcement plumbing*.

## Claim-verification summary

| Area | Claims checked | Confirmed | Partial | Notable partials |
|---|---|---|---|---|
| L1 fast loop | 8 | 8 | 0 | K-selection is by held-out margin (deterministic), not judge score — better than claimed |
| P1.5 acceptor | 10 | 8 | 2 | confirm-path anchor distillation absent (only rollback-path); judgeAccuracy lives on `rsi_status`, not `/health` |
| P2 meta loop | 11 | 10 | 1 | "one change per window" is scheduler-enforced, not ledger-enforced |
| P3 verifier | 7 | 6 | 1 | charter freeze (`IsCharterCase`) has zero production callers — contract is comment-only today |
| L4 self-coding | 9 | 8 | 1 | Go↔Python reopen semantics are copy-pasted mirrors, no parity test |
| Audit/ladder | 6 | 5 | 1 | GRAD excluded from Turning numerator everywhere, but markdown denominator counts it (N/5) |
| P5-1 demand | 9 | 9 | 0 | live-test exclusion verified at source (`client:lt-*`) |

## Findings by severity

File references are repo-relative; all Critical/High findings were
re-verified directly at HEAD, not taken on faith from a single review pass.

### Critical (design-level, undermines a flagship safety property)

**C1 — The e-process can never reject at production parameters; cutover would
silently disable rollback.**
`gateway-go/internal/domain/skills/genesis/eprocess/eprocess.go:20-32` sets
α=0.05 (reject at E ≥ 20) and λ=0.5 with baseline floor 0.05, so the maximum
per-failure factor is 1.475 — reaching E ≥ 20 needs ≥ 8 consecutive
worst-case failures. But the watch resolves as *confirmed* at
`postUses >= rollbackThreshold*2 = 6`
(`gateway-go/internal/domain/skills/genesis/tracker_usage.go:242,264-269` with
`DefaultRollbackThreshold = 3` wired in production): even 6/6 failures yield
E ≈ 10.4 < 20, so `Reject()` is mathematically unreachable before every watch
confirms. Consequences: (a) with `DENEB_EPROCESS_OWNS_ROLLBACK=1` the rollback
mechanism stops firing entirely; (b) pre-cutover, `Disagreement ==
thresholdFired` always, so `EProcessCutoverReadiness`'s agreement rate
(`tracker_eprocess_cutover.go:73-75`) measures the confirm rate, not
mechanism agreement — the readiness gate cannot detect the very defect it
exists to guard. The passing tests use thresholds 8 and 99
(`tracker_eprocess_cutover_test.go:44-47`), masking the production window.
The e-process math itself is correct (E[factor|H0] = 1, Ville applies);
the defect is the confirm-window/α/λ reconciliation.
*Fix direction: scale the confirm window with the e-process sample
requirement, or retune λ/α, before treating readiness as meaningful; add a
production-parameter test.*

**C2 — The "loop must never edit its own acceptor" invariant is enforced on
exactly one field with lexical basename matching; prose, directory, and
scripts-side targets bypass it.**
`gateway-go/internal/domain/skills/genesis/surfaces/surfaces.go:102-120`
reduces each target to `path.Base` before matching, and
`ClassifyProposalSurfaces` inspects only structured `TargetFiles` (empty →
propose-only default, `surfaces.go:141-143`). Verified consequences:
(a) a candidate whose *prose* (`proposedChange`/`evidence`) names
`validation_engine.go` is accepted and dispatched — `scripts/dev/dispatch_prompt.py:59-69`
injects the prose verbatim into a headless agent that lands via the automated
L4 contract with no human approval; `scripts/dev/coding-dispatch.sh:27-28`
explicitly declines to re-check ("이미 forbidden이라 큐에 존재하지 않는다"),
which is untrue for prose-described targets. (b) a directory target
(`.../genesis` — the health miner's normal output shape) classifies as
undeclared/propose-only even though it encloses every forbidden file.
(c) the acceptor set is under-inclusive: `coding-dispatch.sh` (the allowlist
itself), `dispatch_outcome.py`, `pr.sh`, `tracker_self_correction.go`,
`evolution_drift.go`, and CI workflows are all dispatchable.
*Fix direction: path-aware forbidden matching including directory
containment; restate forbidden surfaces inside the dispatch contract or add a
PreToolUse guard in dispatched sessions; add the dispatch/acceptance scripts
to the forbidden class.*

### High

**H1 — Producer epoch can auto-adopt with no bench at all.**
`gateway-go/internal/domain/skills/genesis/meta_evolution.go:433-447`: when
`producerShadowExecutor()` returns nil, the entire shadow-bench block is
skipped, `benchShadow` stays nil, `metaLowConfidenceReason(nil,nil,nil,nil)`
returns `""`, and the flow falls through to auto-adoption. Evaluator and
genesis epochs explicitly *drop* the proposal in the same situation — the
producer epoch is the asymmetric hole. Narrow config (nil primary client with
teacher-fallback propose), but it violates "bench-gated auto-adoption" as an
invariant. No test covers this path.

**H2 — Rollback never re-registers the catalog; the stale entry corrupts the
operator-label chain.** `RollbackSkillWithResult`
(`evolver_regression_rollback.go:207-245`) restores the file but has no
`catalog.Register` call (the evolve commit path has one,
`evolver_candidate_eval.go:234-238`; the catalog is otherwise seeded only at
startup). Chain: watch-triggered baseline-confirmed rollback → organic
false-accept label recorded; catalog still reports the evolved version; the
still-open low-confidence verdict card's liveness check
(`workfeed_meta_proposal.go:97`) passes on stale data; the backup restore
no-ops successfully → a second, operator-attributed label for the same judge
mistake. `assembleJudgeAccuracyEvidence` sums organic and operator labels
independently (`meta_evolution.go:766-785`) — one mistake, two labels, in the
exact evidence stream P3 calibration depends on.

**H3 — Failed rollback is a silent dead end that also poisons labels.**
`maybeFireRollbackLocked` deletes the watch *before* invoking the callback
(`tracker_usage.go:219`); if the rollback then fails (missing backup —
`evolver_regression_rollback.go:216-219` returns without
`LogEvolveRolledBack`), there is no ledger entry, no remaining watch, and the
regressing body stays deployed with only a Warn log. The stashed
`pendingBaselineTest` (`tracker_usage.go:216`) then survives and can attach
its verdict to a *later* resolution of the same skill — a mislabeled
disagreement line in the P1.5 label ledger. Backup-write failure at commit is
also warn-and-continue (`evolver_candidate_eval.go:225-228`), so the missing-
backup path is reachable.

**H4 — The Go/Python audit mirrors already disagree, and parity is enforced
by comments only.** Live drift found: the P5-2 calibration-window constant is
2026-07-12T00:00Z in Go (`rsi_ladder.go:39`) but `1_783_900_800_000` =
2026-07-13T00:00Z in Python (`scripts/audit/rsi_status.py:466`, whose own
comment says 07-12) — bench rows from Jul 12 count toward READY in Go but not
in Python. Additionally: the generated markdown's `Turning: N/{len(layers)}`
denominator counts the GRAD card (`rsi_status.py:579`) contradicting the
"never counts toward the headline" contract; Go caps ledger reads
(`RecentJudgeAccuracy(20)` etc.) while Python scans full files, so metrics
diverge as ledgers grow under the open calibration window; and there is no
shared fixture corpus or cross-language test — the drift above is exactly
what that gap predicts. An audit layer that disagrees with itself undermines
the "auditable RSI" premise.

**H5 — The drift self-brake fails open on persistence errors.**
`Run` ignores the returned drift verdict and the adopt gate re-reads the
persisted marker (`meta_evolution.go:350,500`); if the freeze-transition
append fails (`evolution_drift.go:209-213`) the cycle auto-adopts despite
tripped reward-hacking signals, and `AutoAdoptFrozen` returns false on an
unreadable/corrupt marker (`evolution_drift.go:77-81`). The one component
whose job is to stop the loop under adversarial drift is the one that
defaults to "go" on its own failure.

### Medium

**M1 — Self-minted coverage progressively relaxes the loop's own gates.** The
producer authors both candidate and reproduction case in one response; the
oracle only checks fails-on-original/passes-on-candidate
(`evolver_candidate_eval.go:294-330`) — satisfiable by asserting a phrase the
candidate just added. Adopted cases flip the skill to "covered", dropping the
judge margin 3.0→1.0 (`evolver_candidate_eval.go:432`) and relaxing edit
budgets (`guardrails/guardrails.go`). The weak-case filter checks
concreteness, not discriminative power. This is the one channel where the
loop can loosen its own acceptance pressure over cycles.

**M2 — Fail-open validation stack.** Behavioral replay fails open on executor
error (`validation_engine.go:139-180`); a held-out engine *error* in
preflight logs and continues (`evolver_skill_validation.go:154-159`);
`DENEB_SKILL_EVOLVE_SELFTEST=0` removes the judge. If validation storage
becomes unreadable, the only remaining gates are textual guardrails —
silently. Related: the blind pool falls back to *all* cases when empty
(`validation_engine.go:111-121`, acknowledged tradeoff), re-opening gate-echo
for tiny corpora.

**M3 — Usage-source gate is default-open.** `isRealUsageRecord`
(`tracker.go:316-332`) is an exclusion list; any future recorder that forgets
to tag `Source` counts as real usage and pollutes success rates silently. An
allowlist (or a tag-required invariant) would fail closed.

**M4 — Per-skill evolve is not serialized.** Only `EvolveUnderperformers`
takes `runMu` (`evolver.go:619`); the review-fork and RPC paths can evolve
the same skill concurrently — last-writer-wins on the file, and the backup
can capture an intermediate version, corrupting rollback semantics.

**M5 — Weaken-probe label noise + unenforced charter freeze (P3).** The
`{"must "→"may "}` swap (`judge_subtle_degradations.go:126`) applied in
negated contexts ("must not commit secrets" → "may not commit secrets")
preserves the prohibition — a judge correctly passing it is still ledgered as
a miss, feeding noise into evaluator-epoch evidence. `IsCharterCase`
(`tracker_validation_cases.go:161-167`) is implemented and tested but has
zero production callers — the "structural hedge" is currently vacuous
(evidence feeds counts, not case bodies) and nothing prevents a future
consumer from breaking it silently.

**M6 — Demand-lane evidence: truncation ordering + gameable grounding.**
`assembleDemandEvidence` truncates the digest to 2000 runes
(`curriculum.go:277`) but the failed-request section — "the strongest demand
evidence available" — renders *last* (`runtime/curriculumenv/digest.go:104-186`),
so a busy feed+calendar silently starves it. The 12-rune grounding gate
(`curriculum.go:389-393`) scans the whole evidence block including
self-authored boilerplate (section headers, backlog lines), so a proposal can
"ground" itself on scaffolding. Both cheap to fix; worth fixing while the
calibration window raises curriculum cadence.

**M7 — Allowlist/reopen prefix matching + injection surface (L4).**
`coding-dispatch.sh:328-329` matches graduated sources with `startswith`, so
any caller of `miniapp.self_improvement_coding.record` (source is
caller-chosen) can self-select auto-dispatch by prefixing a graduated
namespace (`health-finding-x`). Mined evidence flows unescaped into a
headless `--permission-mode acceptEdits` session with landing authority
(`coding-dispatch.sh:513-514`) — a prompt-injection channel once
lower-trust sources (runtime error logs) graduate.

### Low / smells (recorded, not expanded)

Meta rollback-watch retry wedge on consumed backups
(`meta_evolution.go:584-592` + `meta_artifacts.go:231-234`); meta watch
metric is global, not artifact-attributable (`meta_evolution.go:567-573`);
`nextEpoch` 10-record scan can reset rotation under verdict bursts; contract
gate anchors are `strings.Contains` prose; judge bench n≤6 with class names
leaked into evidence; genesis bench can adopt on n=1; watch persistence is
atomic but not fsynced (`tracker_usage.go:153`); `DecisionID = skill@version`
swallows a recurring version's second label; verdict card check-then-act race
(`workfeed_meta_proposal.go:88-109`); ladder watch is at-least-once (fires
before snapshot persists, `ladder_watch.go:77-98`); Go 1MB JSONL line cap
errors a whole `Load` while Python parses it; curriculum case-name binding
can orphan pre-authored oracles; provenance (`source=curriculum`) is invisible
to the review-fork proposer prompt (`skill_review_fork.go:231-256`).

## What is genuinely good (verified)

- **The core invariant holds.** Accept/reject/adopt/rollback decisions are
  deterministic Go everywhere inspected; the LLM only produces. Gate order
  (behavioral replay → preflight → flip gate → aggregate → min-delta → judge
  → margin selection → rollback watch) matches the documented contract
  (`validation_engine.go:270-303`).
- **Honesty instrumentation is above the bar for this genre**: DATA-GATED vs
  STARVED distinctions, MANUAL rows for non-ledgered evidence, DEFER-not-swap
  on missing artifacts, "attempted counts in the denominator", the
  gate-exploit trap in `adversarial_coverage.go`, and a ladder engine that is
  structurally read-only (no write path to any lock — verified).
- **Ground rule 3 holds where it counts**: the curriculum/workout lanes write
  zero `UsageRecord`s into the evolver gate; live-test sessions are excluded
  at the source (`agentlog/aggregate_failed.go:43-48`).
- **Provenance/attribution substrate (P1.5) is real**: artifact SHAs, score
  pairs, and margins ride every lifecycle event; organic false-accept mining
  correctly excludes operator rollbacks; verdict-label idempotency is
  enforced and tested.
- **Docs match code.** The roadmap's claim inventory is essentially truthful
  — the failure mode here is not aspirational documentation but
  under-parameterized/under-plumbed enforcement of correctly-described
  designs. (One stale doc note: `self-improvement.md` still calls evolver.go
  "1820 LOC"; it is 763 after the split.)

## Priority recommendations

1. **Fix C1 before any cutover** — reconcile the confirm window with the
   e-process sample requirement and add a production-parameter test; treat
   current readiness labels as invalid for the graduation decision.
2. **Harden the acceptor boundary (C2)** — directory-containment matching in
   `ClassifySurface`, forbidden-surface restatement in the dispatch contract
   (or a session-side guard), and add the dispatch/acceptance scripts to the
   forbidden class. This is the highest-leverage fix per line changed.
3. **Close the unbenched-producer adoption path (H1)** and make the drift
   brake fail closed on marker errors (H5) — both are small, testable diffs.
4. **Re-register the catalog on rollback (H2)** and ledger failed rollbacks
   (H3) — protects the P3 label chain that everything downstream calibrates
   on.
5. **Add a shared Go↔Python fixture corpus for `rsi_status` (H4)** and fix
   the window constant + markdown denominator now.
6. Then the M-tier items, in roughly the order listed — M1 (coverage ratchet)
   deserves a design note rather than a quick patch, since it interacts with
   the reproduction-oracle incentive structure.
