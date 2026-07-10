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
| `LogGenesis` (`tracker.go`) | session pipeline | After a skill-worthy session — records `SkillActivityGenesis`, calls `markSkillAgentCreatedLocked` (`curator.go`) |
| `maybeFireEvolveLocked` (`tracker.go`) | via `LogGenesis` | Event-driven: every `DefaultEvolveEventThreshold` (3) new skills, min-gap respected |
| `EvolveUnderperformers` (`evolver.go`) | `autonomous.PeriodicTask` | Background cycle over underperforming skills |
| `EvolveSkill` (`evolver.go`) | skill-review fork / explicit | One skill, optionally with a `reviewFinding` directive |
| `Nudger` (`nudger.go`) | session runtime, mid-session | Every `DefaultNudgeInterval` (3) tool calls; strictly background |

## End-to-End Flow

1. **Session completes** → `LogGenesis` records the activity and marks the skill agent-created (`tracker.go` → `curator.go:markSkillAgentCreatedLocked`).
2. **Threshold check** → `maybeFireEvolveLocked` fires the evolve trigger at 3 new skills, respecting a min-gap.
3. **Skill generation** → the model generates `SKILL.md`; `Persist` writes to the managed dir. `ErrSkillDeduped` (`genesis.go:41`) is an intentional skip, not a failure.
4. **Catalog registration** → discovered by precedence `bundled < managed` (see `skills/CLAUDE.md`).
5. **Evolution** → `Evolver.EvolveSkill` / `EvolveUnderperformers` rewrites; a candidate is judged by an independent judge model (`SetJudge`).
6. **Validation gates** → `evolver_skill_validation.go`: the self-harness audit must name a target failure signature, and `validateSelfHarnessEditedSurface` verifies the claimed `edited_surface` matches the section actually changed.
7. **Post-evolve watch** → the tracker watches next uses; `DefaultRollbackThreshold` (3) consecutive failures → `RollbackSkill` restores the backup.
8. **Curation** (`curator.go`) → staleness lifecycle `active → stale → archived` at `staleAfterDays` (30) / `archiveAfterDays` (90).

## Key Files

| File | Role |
|---|---|
| `evolver.go` | `Evolver` — rewrite orchestrator (1820 LOC, largest file) |
| `tracker.go` | `Tracker` — activity log, lifecycle, event triggers, usage-source gate |
| `genesis.go` | `Service`, `Config`, generation/persist orchestration |
| `curator.go` | Staleness classification + archive; `markSkillAgentCreatedLocked` |
| `nudger.go` | Mid-session background nudging (shares Service cooldown / `MaxSkillsPerDay`) |
| `evolver_adopt.go` | Copy-on-evolve **adoption** of bundled skills (`SetAdoptionDirs`) |
| `evolver_skill_validation.go` | Deterministic admissibility gates — **pure**, no `Evolver` state |
| `editable_surfaces.go` | `EditableSurface`, `ClassifySurface`, `DeclaredEditableSurfaces` whitelist |
| `workout.go` | Synthetic exercise lane — evidence only, never real usage |
| `tracker_self_correction.go` | Self-correction candidate record/query + forbidden-surface gate |
| `tracker_recurrence_promotion.go` | Recurrence/cluster → self-correction candidate promotion |

## Surface Tiers (`editable_surfaces.go`)

| Tier | Meaning |
|---|---|
| `auto-apply` | Loop may promote edits itself, behind gates |
| `propose-only` | Record a proposal only |
| `forbidden` | Never self-editable; rejected at record time so misaimed candidates never queue |

`DeclaredEditableSurfaces()` is ordered by precedence (first match wins); forbidden
entries lead so they cannot be shadowed. `classifyProposalSurfaces` applies this
gate when a candidate is recorded — source code is never a self-edit target.

## Usage Sources — the success-rate gate (`tracker.go`)

Only `real` usage feeds the evolver's success-rate gate. Conflating the review
fork's self-activity with real-world outcome drove the email-analysis evolve
thrash (`tracker.go:58`, PR #2328 — 6 evolves in ~2 days on one skill, undetected).

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
- `selfCorrectionReopenBlocked` allows an **applied** candidate to reopen only
  after a cooldown once the same signature recurs (fixed-but-still-failing);
  **rejected** never reopens (operator veto is respected). Per-tick promotion is
  capped. (PR #3367; drain-side hardening PR #3380.)

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

## Where to Look Next

- `skills/CLAUDE.md` — discovery precedence (bundled < managed), workspace overrides
- `tracker_optimizer_memory.go` — slow-memory injected into the evolve prompt
- `tracker_rejected_edits.go` — rejected-edit buffer fed back into evolve prompts
- server-side capture wiring (`promoteRecurrences`, heartbeat self-improve sweep)
  lives in `runtime/server`, outside this package
