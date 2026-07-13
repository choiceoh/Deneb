# RSI Research Addendum — post-H1-sweep papers (2026-07-12)

> Follow-up to `rsi-research-2026h1.md` (118 papers, cut off ~2607.07). This
> addendum covers papers the H1 sweep missed or that appeared after its cutoff,
> mapped against the codebase as it stands after the P5 slices, the e-process
> cutover mechanism (#3550), and utility-based archiving (#3547). Verified via
> arXiv API 2026-07-12; every ID below is absent from the H1 sweep's ID list.

## Deep mappings (actionable)

### 1. The Blind Curator (2607.07436, 2026-07-08) — HIGHEST PRIORITY

Finding: **false-pass bias in the judge silently disables contribution-based
skill retirement past a sharp threshold** — and stays hidden because evaluation
quality only visibly degrades when the same bias also breaks synthesis.
Countermeasure: a cheap defect-injection audit of the signal feeding
*retirement*, run before trusting the retirement lane.

Deneb mapping: our retirement lane is the utility archiver (#3547,
`curator_utility.go`) triggered by **rollback thrash** — which depends on
post-evolve failures — which depend on the usage-success labeler. That labeler
(`SkillConsultLog` → `RecordSkillUse`) is deterministic but LENIENT: a turn
with no tool error marks every consulted skill successful, so a skill giving
bad guidance through clean tool calls is systematically labeled a success.
This is exactly the paper's false-pass structure, one level below the LLM
judge (which the judge-accuracy lane already audits — the labeler is not).

**Next-commit candidate (spec):** a deterministic *labeler blind-spot audit* —
join `evolve_confirmed` lifecycle entries against the workout lane's
fails-own-validation records for the same skill in the same window. A skill
the rollback watch confirmed CLEAN while it persistently fails its own
held-out cases is a labeler false-pass suspect. Ledger-join only (no LLM, no
synthetic usage in real stats — ground rule 3 intact); surface the count on
`rsi_status` L1 and as evidence in the evaluator epoch. Prereq: read
`workout.go`'s record format before implementing the join.

### 2. RSEA held-out selection (2606.28374, 2026-06-17)

Monotone-safe recursion: strict keep-better gate on a disjoint held-out split,
with fallback to the un-evolved baseline when evolved context would hurt
(their no-gate ablation collapses 70.7%→0.14 across benchmarks). Deneb's
visible/blind pools + flip gates already implement the core. The transferable
delta: RSEA evolves THREE natural-language layers (strategy / skills /
playbook) — Deneb evolves only SKILL.md bodies. Our `heartbeat-instructions`
surface is declared propose-only "auto-apply awaits P2" (`surfaces.go`) — and
P2's bench-gated auto-adoption now EXISTS. Graduating HEARTBEAT.md to gated
auto-apply is now a mechanism-complete decision awaiting only (1) a
shadow-replay gate equivalent for heartbeat turns and (2) explicit operator
approval per the surfaces.go contract. This is the highest-leverage
recursion-surface widening available (P5-4).

### 3. Useful Memories Become Faulty (2605.12978) — validated, no action

Continuous LLM re-consolidation corrupts memory (utility rises then falls
below no-memory; keep raw episodes first-class, gate consolidation).
Deneb's optimizer memory (`tracker_optimizer_memory.go`) is deterministic
counters + capped direction strings RE-DERIVED from the append-only lifecycle
ledger — raw episodes stay the source of truth, no LLM consolidation loop.
Structurally immune; keep it that way (a future "smarter memory" proposal that
adds LLM consolidation should cite this paper as the reason to decline).

### 4. Evaluator Preference Collapse (2606.16682) + Hack-Verifiable Environments (2605.20744)

Collapse: evaluator preferences acquired in one modality corrupt selection in
another; strategy weights collapse onto one favorite. Deneb's analog risk is
the single judge model grading every skill domain — the drift brake watches
adoption-diversity collapse already, but per-DOMAIN judge accuracy is not
segmented. Cheap slice: segment the judge-accuracy scoreboard by skill
category so a category-local bias can't hide in the aggregate.
Hack-verifiable: plant *exploit-shaped candidates* (game the validation cases
without improving the skill) and verify gates reject them — extends the gate
fuzz (#3436) from crash bugs to reward-hack traps; the adversarial-coverage
lane is the natural home.

### 5. Tree-of-Experience (2606.06960) + Experience Graphs (2606.29823)

Structured (tree/graph) experience beats flat lists precisely in the
low-repetition, delayed-feedback regime Deneb lives in. The meta-experience
ledger and exemplar retrieval (GRAO) are flat JSONL + recency. Candidate:
key exemplar retrieval by failure-signature cluster (the
`FailureEvidenceClusters` machinery already exists) instead of recency-only —
retrieval policy is also a named P5-4 externalization candidate, so structure
and evolvability can land together.

### 6. ANCHOR / Healthy Evolution (2606.06114) — P5-5 validation

Human intervention is most effective at the OUTPUT-VERIFICATION phase, and
calibrated low-frequency oversight preserves safety without impeding learning.
Validates the feed-card post-hoc veto design (intervention exactly at
verification, not generation). Delta worth stealing: route only
LOW-CONFIDENCE adoptions (thin bench margins) to a feed card that requests an
explicit verdict, rather than uniform notification — raises the value of each
scarce operator interaction (the P5-5 signal is currently ~0/week).

## Noted, lower priority

| Paper | Takeaway for Deneb |
|---|---|
| 2607.08010 Tool-Making in Low-Latency Systems | P4 pairing precedent; revisit when L4 source auto-apply opens |
| 2607.07321 Atomic Actions → SOPs | Tool-sequence mining → skill procedure sections; curriculum lane could mine SOPs from transcripts |
| 2606.06324 HarnessFix | Trace IR + harness-vs-model failure attribution; enrich `runtime_error_mining.go` candidates with component attribution |
| 2606.06741 OpenSkill | Verification-anchors-from-docs pattern; curriculum lane already authors cases first — steal the anchor-extraction step for doc-grounded skills |
| 2605.26321 Anchor (benchmark drift) | Single-spec generation of instruction+oracle+verifier; audit that judge-degradation pairs and shadow scenarios stay mutually consistent as the catalog moves |
| 2605.24426 SEAL | Turn-level failure diagnosis feeding BOTH sides; Deneb analog = failure clusters feeding evolve prompts (exists) and validation-case authoring (exists) |
| 2606.04465 SePO | Archive-of-prompts as stepping stones for L2 — meta-experience ledger already plays this role; K>1 meta candidates per cycle is the delta |
| 2604.14717 Layered Mutability | "Compositional drift" + identity hysteresis 0.68 — restoring a prompt does NOT reset behavior accumulated via memory; strengthens the case for the frozen meta-governor (L5 keep-frozen) and for auditing MEMORY-layer drift, not just artifact versions |
| 2602.10226 Self-Evolving RecSys (YouTube) | Offline proxy-metric inner loop + online north-star outer loop = industrial precedent for P5-5's advisory→gate promotion path |

## Next-commit candidates, in order

1. **Labeler blind-spot audit** (Blind Curator) — deterministic ledger join,
   evidence-only, `rsi_status` L1 metric + evaluator-epoch evidence line.
2. **Per-category judge-accuracy segmentation** (Preference Collapse) —
   additive fields on `JudgeAccuracyRecord.ByClass` keyed by skill category.
3. **Reward-hack trap corpus in adversarial coverage** (Hack-Verifiable) —
   planted exploit-shaped candidates that must be rejected; extends #3436.
4. **HEARTBEAT.md gated auto-apply** (RSEA) — needs operator approval +
   heartbeat shadow-replay gate; the P2 machinery is otherwise ready.
5. **Low-confidence adoption → explicit operator verdict card** (ANCHOR) —
   thin-margin adoptions request a verdict; densifies the P5-5 signal.
6. **Cluster-keyed exemplar retrieval** (ToE/ExpGraphs) — pairs with the
   P5-4 retrieval-policy externalization.

## Second pass (2026-07-12 저녁) — additional angles

Follow-up sweep on angles the first pass did not mine: skill LIBRARIES
(retrieval/acquisition/grounding), judge calibration mechanics, and the
text-vs-code experience boundary. Dedup verified against both prior ID lists.

### Deep mappings

**BINEVAL — Ask, Don't Judge (2606.27226).** Decompose the judge verdict into
atomic BINARY questions answered independently, then aggregate: calibrated,
ceiling-free scores plus question-level feedback that directly drives
evaluator-prompt refinement. Deneb's skill judge emits pass + scalar 0-100
score pairs — scalars are exactly the miscalibration surface the
judge-degradation bench keeps probing. Two adoption paths: (soft) feed the
binary-question rubric as an exemplar direction into the next evaluator
epoch — zero code; (hard) migrate the judge response schema to per-question
verdicts — touches `metaArtifactContracts` anchors + the parser, so it is a
reviewed code change, but yields question-level L3 labels (which QUESTION the
judge got wrong, not just that it missed).

**Source-grounding gate for curriculum skills — RESOURCE2SKILL (2606.29538) +
SkillCenter (2607.07676).** Both ground acquired skills in sources;
SkillCenter's defining rule: *"each retained claim maps to an exact quotation
in its source."* Deneb's curriculum lane (P5-1) already authors validation
cases first; its `evidence` field is free prose an LLM can hallucinate. The
cheap deterministic honesty upgrade: require the proposal's evidence to QUOTE
the environment digest verbatim and reject at record time on a failed
substring check — the reproduction-oracle pattern applied to demand evidence.

**SOP miner — EvoSOP (2607.07321) + Tool-Making in Low-Latency Systems
(2607.08010) + Metis (2606.24151).** Convergent finding: promote REPEATED
multi-step procedures into validated, versioned tools OFFLINE (pre-deployment
pipeline, labeled cases from traces, fallback to raw generation), and convert
text-experience to code only when reuse frequency justifies the cost (Metis'
promotion rule; 42-62% latency and up-to-53% error reductions in production
alarm triage). Deneb mapping: a deterministic transcript miner that detects
recurring tool-call sequences (frequency-thresholded) and files propose-only
scope=code L4 candidates proposing a composite tool, evidence = the observed
traces — proactive L4 supply (P5-3) with the paper's frequency gate keeping
it honest.

### Noted

| Paper | Takeaway |
|---|---|
| 2607.06283 skill-retrieval reranking | decomposition-guided rerank vs ambiguous name matching — revisit when the catalog outgrows the prompt index (today it fits) |
| 2606.29823 Experience Graphs | ledgers as queryable DB with causal lineage (Meta KernelEvolve ~10x); JSONL + wiki-graph cover the near term — infra-heavy, note-only |

### Second-pass candidates, in order

1. **Curriculum evidence source-grounding gate** — deterministic verbatim-
   quotation check on curriculum proposals (small; curriculum.go record gate).
2. **Binary-question judge direction** — soft path first (evaluator-epoch
   exemplar), schema migration as a reviewed follow-up.
3. **SOP miner → composite-tool L4 candidates** — frequency-gated transcript
   mining into the existing review lane (medium; new miner, no gate changes).
