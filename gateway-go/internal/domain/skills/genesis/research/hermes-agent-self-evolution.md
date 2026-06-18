# Hermes Agent Self-Evolution transfer

Source: <https://github.com/NousResearch/hermes-agent-self-evolution> and
<https://hermes-agent.nousresearch.com/docs/user-guide/features/skills>

## Narrow lesson

Hermes' useful transfer is not the full DSPy/GEPA runtime. Deneb already has a
production Go evolver with held-out validation, Self-Harness audit fields,
rollback watch, rejected-edit memory, and Propus status surfaces. Pulling a
Python optimizer into that hot path would add operational weight before Deneb
has a separate experiment lane.

The transferable part is the promotion discipline:

- generate targeted variants from execution traces, not broad rewrites;
- prefer patch-sized edits over whole-skill redesigns;
- enforce constraint gates before promotion;
- keep candidate writes review-visible;
- reject semantic drift from the original skill purpose;
- keep skill artifacts small enough to preserve prompt/cache behavior.

## Deneb mapping

`PropusDoctrine()` records Hermes as an operational-transfer source, not a paper
source. The corresponding invariant is:

`hermes_style_evolution_is_patch_first_size_bounded_and_review_visible`

The deterministic gate lives in `validateHermesEvolutionGuardrails`:

- frontmatter plus candidate body must stay under 15KB;
- the top-level skill title must not drift;
- mature skills may not change more than three sections in one automatic evolve.

LLM prompts mirror the same rule, but the deterministic preflight is the
authority. If the model proposes a broad rewrite, the candidate is rejected and
the rejected-edit buffer preserves the failure reason for the next pass.

## Explicitly deferred

Deneb does not yet import Hermes' full external optimizer:

- no DSPy/GEPA dependency in the production gateway;
- no external Darwinian Evolver code path;
- no automatic tool-description/system-prompt/source-code evolution from this
  path.

Those belong in a future experiment lane that emits Propus candidates, not
direct production skill writes.
