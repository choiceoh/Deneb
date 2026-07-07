---
name: self-evolve
version: "1.1.0"
category: coding
description: "Cross-surface self-evolution control plane. After a failure or notable success, attribute it to the right surface of Deneb's composite policy (model-role / prompt / skill / memory / tool / routing / guardrail), fix at the CHEAPEST surface, and pass a validation gate before committing the lesson. Use when: (1) a session exposed a failure and it's unclear which surface owns the fix, (2) you're about to commit a 'lesson' (memory/skill/prompt) and it should be validated first, (3) post-incident review. NOT for: skill-only edits (use skill-evolution), durable user facts (→ wiki/memory), or committing an unverified diagnostic conclusion."
metadata:
  {
    "deneb":
      {
        "emoji": "🧠",
        "requires": { "bins": ["jq", "python3"] },
        "tags": ["self-evolution", "control-plane", "credit-assignment", "ATDP", "validation-gate", "counterfactual-replay", "composite-policy", "cheapest-surface", "intervention-surface", "self-correction-queue"],
        "related_skills": ["skill-evolution", "evolution-proposal", "session-logs"],
      },
  }
user-invocable: true
---

# Self-Evolve — Cross-Surface Control Plane

Deneb is a **composite policy**, not just an LLM. When something fails (or works
notably), the fix rarely belongs to the model — it belongs to some *surface*.
This skill is the control plane that (1) attributes the outcome to the right
surface, (2) picks the **cheapest** intervention, and (3) refuses to commit a
change until it clears a **validation gate**.

Grounded in *"Next-Generation Agentic RL Systems Enable Self-Evolving Agents"*
(Ant Group / HKUST / Tsinghua, arXiv 2607.01120): self-evolution ≠ retraining
weights; a deployed agent = base LLM + in-context harness + memory + tools +
guardrails, and different failures need different intervention surfaces. This
skill is that paper's **evolution control plane + ATDP credit assignment**,
scaled to a single-user Deneb.

It **complements, does not replace** `skill-evolution` (harness surface only) and
`evolution-proposal` (skill routing). This one routes across *all* surfaces and
adds the **memory validation gate those lack**.

## When to Use

- A session exposed a failure and it is unclear WHICH surface owns the fix — was
  it the model, the prompt, a skill, a stale/wrong memory, a tool, routing, or a
  guardrail?
- You are about to commit a **lesson** (a memory/wiki entry, a skill patch, a
  prompt edit) and it should be validated before it becomes durable.
- Post-incident review of a notable failure, or capture of a reusable success.

Do NOT use for pure skill edits (use `skill-evolution`), durable user facts
(→ wiki/memory directly), or when you would commit a lesson you cannot verify.

## Deneb's composite-policy surfaces (credit-assignment targets)

| Surface | Deneb locus | Cheapest fix | Validation gate |
|---|---|---|---|
| **π model-role** | `docs/agent-rules/model-roles.md`, wormhole routing | reassign a role / effort-thinking route (NOT weight training) | live smoke on the role |
| **H prompt** | `prompt/system_prompt.go`, context files | prompt / context-file edit | prompt-cache safe + live-test |
| **H skill** | `skills/**` | patch a skill → **delegate to `skill-evolution`** | its held-out-replay gate |
| **M memory** | wiki / diary / polaris, `memory/*.md` | write / correct a memory | **★reproduce the claim before commit** (Pitfalls) |
| **T tool** | `toolreg/tool_schemas.json`, `tools/*.go` | schema/description edit or bugfix | `make check` + `live-test.sh smoke` |
| **routing** | wormhole entries, effort/thinking toggle | entry / toggle edit | APC byte-invariance + prefix-cache hit |
| **G guardrail** | proactive-gate, silent-reply, safety config | config edit | no over/under-fire on a replay set |

## Procedure

1. **Capture the trajectory (ATDP-lite).** Pull the session via `session-logs`.
   Reduce it to typed events: **observation** (input / tool output) →
   **action** (what the agent did) → **outcome** (tool return / user
   accept-reject / error) → **reward signal** (user correction, failing check,
   or success). The signal may be **delayed** (a correction 3 turns or 3 days
   later) — attach it to the original event, do not lose it.

2. **Assign credit — which surface?** Ask the paper's question: *which
   observation / prompt-fragment / retrieval / tool-call / memory-item /
   guardrail caused it?* Name ONE primary surface. Only if genuinely
   cross-cutting (≥2 surfaces) may a deeper (π) fix be justified.

3. **Route to the cheapest surface that fixes it.** Order (cheap → expensive):
   prompt/memory edit → skill patch → tool schema → routing → model-role →
   (single-user: essentially never) weight training. Match the failure to the
   SMALLEST surface. Reaching for the model when a prompt/memory edit suffices is
   the exact anti-pattern the paper names.

4. **★Validation gate — counterfactual replay before commit.** Do NOT commit the
   lesson until you have shown it would have prevented the failure (or that the
   success is reproducible):
   - **Memory (M):** re-derive the claim from evidence; if it is a technical
     conclusion, run the actual check **with a control**. *(This is the gate
     Deneb lacked: a "GLM-OCR can't do Korean" memory and an "MTP is useless"
     memory were both committed without a clean control/A-B — and both were
     wrong, corrected only later. Never commit a diagnostic lesson you have not
     reproduced.)*
   - **Harness / tool / routing:** existing gates — `skill-evolution`
     held-out-replay, `make check`, `live-test.sh smoke`, APC byte-invariance.
   - Fails the gate → **do not commit**; park it in the self-correction queue as
     a `**Why:** unverified` hypothesis, exactly as `skill-evolution` keeps
     rejected edits for future input.

5. **Apply + record with provenance.** Make the ONE cheapest change. Record the
   lesson with its causal chain (observation → surface → fix → validation
   result) so it is replayable/auditable — the ATDP metadata principle. One
   change at a time; never bundle surfaces.

## Reward harvesting (`scripts/harvest_rewards.py`) — the ATDP `r` layer

Deneb session JSONL already carries `o/h/a/y/m` (user msgs, thinking, tool
calls, tool results, usage). The one ATDP field it captures NOWHERE is `r`
(reward) — the signal that makes trajectories learnable. This bundled script
harvests late-bound reward signals from `~/.deneb/agents/*/sessions/*.jsonl`
and credit-assigns each back to the action that earned it (a correction at turn
T attaches to the assistant action at T-1):

- **`user_correction`** — a user turn negating/redirecting the prior assistant
  action (precision-first Korean regex; plain "아니" substring over-fires so it
  is anchored).
- **`self_correction`** — the agent reversing an earlier claim (the archetype:
  the GLM-OCR "can't do Korean" and "MTP is useless" reversals — both wrong
  lessons the user had to catch).
- **`tool_failure`** — a toolResult with the structured `isErr` flag ONLY.
  Text-scanning result content is a false-positive trap: tool results routinely
  carry code/docs that *mention* "Error:"/"cannot"/"panic:" (validated on real
  sessions — 11/11 text hits were false), so trust only `isErr`.

```bash
ls ~/.deneb/agents/*/sessions/*.jsonl | xargs skills/coding/self-evolve/scripts/harvest_rewards.py
skills/coding/self-evolve/scripts/harvest_rewards.py SESSION.jsonl --json   # structured records
```

**Validated 2026-07** (dogfooded through this very loop): detection fires on
synthetic positives and produces **zero false positives** on real sessions after
the isErr-only fix. Recall depends on sessions carrying signal — sparse/clean
histories yield none, which is honest, not broken. The harvested reward record
is the credit-assignment + eval-corpus input for the rest of the loop.

## Pitfalls

- **Unvalidated memory is the #1 gap.** Skill edits pass a gate; memory writes
  historically did not → wrong lessons ossify. A lesson's claim must be
  **reproduced** first. If you can't reproduce it, write it as a
  *question/hypothesis*, not a *fact*.
- **A-B hygiene.** When measuring an intervention, toggle **only that variable**
  on the **same** config. (The "MTP useless" error came from comparing MTP-on
  runs across *different* configs — never a clean MTP-off baseline.)
- **Surface confusion.** "The model got it wrong" is usually false — the prompt
  didn't anchor it, a memory was stale, or a tool didn't substitute. Diagnose the
  surface before reaching for the model.
- **Cheapest-first, not deepest-first.** For one user you essentially never
  retrain; the model surface = role reassignment at most.
- **Delegate, don't duplicate.** Harness/skill fixes go through
  `skill-evolution` / `evolution-proposal`; this skill only routes and adds the
  memory gate.

## Verification

- The committed change names its surface and shows its validation evidence (a
  reproduced check, a passing gate, or a live-test line).
- Re-running the original trajectory (or its check) no longer fails.
- Nothing was committed that could not be reproduced.
