# L5 — Un-freezing the meta-governor (design-only; recommendation: keep frozen)

**Status: DESIGN / ANALYSIS (2026-07-12).** This document explores the single
element that sits one level above L4 in the RSI ladder: the **meta-governor**,
the one prompt L1–L4 all treat as fixed. It is a design exploration, not a
change. It touches no code. Its conclusion is that the governor **stays a
compiled constant** for now, and it states precisely what evidence would flip
that. Canonical roadmap: [recursive-self-improvement-roadmap.md](recursive-self-improvement-roadmap.md).

## What the meta-governor is

`metaEvolutionSystemPrompt` — a compiled constant at
`gateway-go/internal/domain/skills/genesis/meta_evolution.go:666` — governs the
slow loop's *producer*: the LLM call that rewrites the skill-improvement
pipeline's own system prompt (the L2 evolve artifact). Its own source comment is
the whole design in one sentence:

> Deliberately a compiled constant, NOT a meta artifact: the loop must not edit
> its own governor (self-reference guard, at least until P3's verifier
> co-evolution brings independent oversight).

The RSI loops run on three *dedicated* prompts, in two freeze tiers:

| Prompt | Layer | Freeze tier | Who edits it |
|---|---|---|---|
| skill-body prompts | L1 | evolvable | genesis (fast loop) |
| skill-improvement pipeline prompt (evolve) | L2 | evolvable | the meta-loop, weekly |
| judge prompt | L3 | evolvable | co-evolution from labels |
| **meta-governor** (`metaEvolutionSystemPrompt`) | **above L4** | **frozen** | **humans, via PR** |

So "do we need a dedicated RSI prompt?" is already answered — there are three.
The open design question is narrower and sharper: **should the governor of the
meta-loop become the fourth evolvable prompt?** That is what "L5" means here.

## Why this is the top of the recursion

L1–L4 each improve something *below the acceptor*: skills, the procedure, the
judge, the source. Every one of them holds the governor fixed, because the
governor is what decides *how the procedure itself is allowed to change*.
Un-freezing it means the recursion editing its own governor — the deepest
self-reference in the system, and the exact shape the unbreakable principle
forbids elsewhere:

> The acceptance machinery (`validation_engine`, `eprocess`, meta benches,
> `judge_accuracy`, `tracker_usage`, `surfaces`, `meta_evolution`) is a
> forbidden self-edit surface — changed only by human PR. *The circuit that
> decides accept/reject must not be optimized by what it accepts.*
> (`gateway-go/internal/domain/skills/genesis/CLAUDE.md`; `surfaces.go`.)

The governor is one level *above* even that circuit: it produces the producer
whose output the circuit judges. Optimizing it against the loop's own results is
the highest-leverage — and highest-risk — self-reference available.

## The precondition is formally met but substantively unmet

The freeze comment names its own release condition: "until P3's verifier
co-evolution brings independent oversight." P3 (verifier co-evolution) is now
**built** — `judge_accuracy.go`, charter-case exclusion (`IsCharterCase`), the
three 2026H1 preconditions (baseline-aware rollback, fuzz-before-co-evolving,
frozen charter cases). So the letter of the precondition is satisfied.

The **substance** is not. Independent oversight is only oversight if it is
*calibrated* — and P3 is currently data-gated to zero. Verified on the live
ledger (`~/.deneb/data/skill_genesis_log.jsonl`, 2026-07-12):

- 26 `evolved` in the last 30 days, **0 `evolve_confirmed`, 0 `evolve_rolled_back`**.
- Therefore **0 judge-miss labels** for P3 to learn from (a label is a
  false-accept = rollback, or a false-reject = strong post-rejection usage).
- The verdict famine is DATA-GATED, not broken: confirmation requires real
  post-evolve *uses*, which at single-user volume accumulate slowly
  (`confirmEvolve` needs `uses/fails/recurred`; watches resolve via
  `ResolveStaleWatches`, scheduled at `evolver.go:715`). All 26 watches are
  0.9–29.5d old — in-flight, not stuck.

The consequence for L5 is decisive: **you cannot safely unfreeze the governor
under an oversight mechanism that has never made a graded call.** Doing so would
optimize the meta-loop's governor against an uncalibrated judge — the precise
false-accept drift (PACE 2606.08106; the roadmap's P3 precondition 2) that the
entire architecture is built to prevent, injected at the most sensitive point.

## Staged unfreeze ladder (design — how it *could* be done, later)

If and when the substance is met, this is the safe shape — mirroring the
roadmap's graduation ladder (autonomy expands on track record, never on
calendar). Each stage is a lock; the evidence flips it.

| Stage | State | Evidence to graduate |
|---|---|---|
| **0 (today)** | Frozen compiled constant. Improvements to it ship as human PRs. | — |
| **1 propose-only** | The meta-loop may *emit* a governor revision to the lifecycle log / review-fork. **Never applied.** A human ratifies via PR. | P3 shows ≥20 disagreement labels at ≥90% legacy-agreement (mirrors the e-process graduation row). |
| **2 shadow** | The proposed governor runs in **shadow** against held-out `meta_judge_bench` / `meta_producer_bench`; scored, not adopted. Score accrues as evidence. Charter meta-cases stay held-out and excluded from any co-evolution (SkillAudit hedge). | Shadow governor beats the incumbent on held-out margin over K≥3 epochs, with no charter-case regression. |
| **3 gated auto-adopt** | Auto-adopt on the slow cadence, under the drift brake + meta rollback watch, one-change-per-window. | The deploy-watch/rollback path exercised end-to-end at least once (real or fire-drill). |

Even at Stage 3, three things **never** move — they are the constitution, not
the policy the governor may tune:

1. The governor's **output JSON schema** (the parser contract). Prose only.
2. The **forbidden-surface list** (`surfaces.go`) — moving your own guardrails
   is the collapse vector, not a self-improvement.
3. The governor's **safety clauses** — "one weakness per cycle," "skip if
   unsure," "never change the schema contract." These are what make its own
   revisions bounded; a governor that can delete its own brakes is unbounded.

## What must never unfreeze — even in principle

Two elements are permanently human-governed, independent of any track record:

- **The objective function** — what "good" *means*. A loop that can rewrite its
  own reward will drift its own target (reward hacking's root). The roadmap's
  workstream 5 keeps operator-visible utility ADVISORY, never a gate, for
  exactly this reason.
- **The forbidden boundary itself** (`surfaces.go` / `editable_surfaces.go`).
  The system may *propose* changes to what is self-editable; only a human ratifies
  them. This is the load-bearing invariant that keeps L1–L4 trustworthy.

## Recommendation

**Keep `metaEvolutionSystemPrompt` frozen.** The "until P3" gate is met in form
but not in substance: P3 has zero graded labels, so its oversight is unproven,
so unfreezing now would be optimizing the governor against an uncalibrated
verifier — the one move the whole design forbids. Improvements to the governor
continue to ship as human PRs, as they do today.

Revisit L5 Stage 1 (propose-only) when **both** hold:

1. The verdict famine resolves — `evolve_confirmed` / `evolve_rolled_back`
   verdicts begin accumulating (the RSI-status L3 lane turns from DATA-GATED to
   LIVE), *and*
2. P3 co-evolution shows ≥20 disagreement labels at ≥90% legacy-agreement.

Until then the highest-value L5 work is not un-freezing anything — it is
**feeding the loop** (roadmap P5 workstream 1, demand generation) so the
verdicts and labels that would *justify* an unfreeze can exist at all. The
governor is not the bottleneck; fuel is.
