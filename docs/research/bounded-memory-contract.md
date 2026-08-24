# Bounded Memory Contract (typed-layer vocabulary)

Adopted vocabulary and concepts from AgenticSTS (arXiv:2607.02255), by operator
direction (2026-08-25). The paper's own results are directional (N=10,
p≈0.37) and are NOT the reason this page exists; its *frame* is. Deneb has
been growing memory surfaces one incident at a time — skills, recall, facts,
blackboard, diary — and this page names the system they already form, so
future design conversations (above all the context-pressure spike) argue in
one vocabulary instead of five.

## The frame

**Memory is a contract about what each future decision is allowed to see.**

Two contract families:

- **Accumulating contract** — every decision sees the raw transcript of every
  prior decision. Simple, and the default of every chat loop. Its failure
  mode is measured in our own fleet: 49% of long-horizon runs hit
  context-pressure (mid-run compaction or tool-result truncation) —
  `scripts/audit/longrun-failure-taxonomy.py`, 28d window, #4684.
- **Bounded contract** — every decision gets a fresh prompt assembled from
  TYPED layers by retrieval; no raw cross-decision transcript. Prompt size is
  O(layers × caps) regardless of run length, and any layer can be ablated in
  isolation.

Deneb's interactive chat is, and stays, an accumulating contract — a
conversation IS a transcript, and the prompt-cache doctrine
(`docs/agent-rules/prompt-cache.md`) is built around append-only history.
The bounded contract is the design target for **autonomous long-horizon
execution**: subtask executors, background lanes, and any place #4684's
context-pressure lives.

## The typed layers, mapped to Deneb

| Layer (AgenticSTS) | What it is | Deneb surface today | State |
|---|---|---|---|
| **L₁ Protocol** | immutable role/protocol templates | system prompt static block (`prompt/system_prompt.go`) | held |
| **L₂ State schema** | typed current-state slots + legal actions, externally maintained | `toolport/blackboard.go` — typed keys + step contracts, but **run-scoped and prompt-invisible** | partial — the spike's target |
| **L₃ Domain knowledge** | enumerable rule/reference data, refreshed out-of-band | wiki tier-1 + topic knowledge + context files | held |
| **L₄ Episodic** | post-run summaries indexed by *situation class*, retrieved by situation | retry-correction hints (#4672: error-signature → prior fix) are the one true L₄ lane; diary is unindexed prose | partial |
| **L₅ Skills** | prose policies behind **boolean triggers**, written through deterministic gates | `skill_hints.go` literal triggers + genesis validation/rollback gates | held — independently convergent (they also rejected similarity-RAG for triggers, the exact choice skill_hints made) |

Two readings of this table matter:

1. **We already run L₁/L₃/L₅ as a bounded stack** — what accumulates is only
   the execution transcript. The missing piece is exactly one: L₂ state that
   *persists across decisions and enters the prompt*, replacing the transcript
   as the carrier of task state.
2. **Layer priority is measured, twice.** AgenticSTS's largest separation was
   no-scaffold → L₅ (3/10 → 6/10), with L₄ saturating on top of it. Our own
   #4684 shows the dominant failure is state-in-context, not missing episodic
   recall. Both point the same way: **invest in L₂ and L₅; treat L₄ as
   garnish.**

## Design consequences (the context-pressure spike)

The B1 spike ("blackboard 세션 승격") is now specified in this vocabulary:

- **Goal**: for a long-horizon run, task state lives in L₂ (typed blackboard
  slots + step contracts), and subtask executors receive fresh prompts
  assembled as π(L₁, L₂, L₃-selection, L₅-triggered) — not the parent's
  transcript.
- **Do first**: L₂ promotion (persist blackboard across the run's decisions,
  render it into executor prompts). L₅ already rides in via skill hints.
- **Do NOT do first**: an episodic-injection layer (L₄ dumps of past runs).
  Both evidence sources above say it pays last; it also has the largest
  false-positive surface (wrong lesson at the wrong moment steers the run).
- **Ablation discipline**: the bounded contract's practical virtue is that
  layers can be turned off one at a time. The spike ships with per-layer
  kill switches so its benefit is attributable, not vibes.
- **Out of scope**: interactive chat sessions. The accumulating contract with
  compaction stays; this work must never touch the prompt-cache path.

## Vocabulary adopted

Use these terms in design docs, PR bodies, and reviews for memory-adjacent
work: *memory contract*, *accumulating vs bounded contract*, *typed layer
(L₁ protocol / L₂ state / L₃ knowledge / L₄ episodic / L₅ skills)*,
*write gate* (deterministic admission for L₄/L₅ writes — genesis's gates are
write gates), *fresh-prompt executor*. A memory feature proposal should say
which layer it extends and which contract it assumes.
