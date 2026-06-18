---
name: skill-evolution
version: "1.0.7"
category: coding
description: "Evolve and optimize existing skills using autoresearch methodology. Use when: (1) a skill produces suboptimal results, (2) a skill's instructions are outdated or incomplete, (3) systematic improvement of skill quality is requested. NOT for: creating new skills (use skill-factory), cosmetic changes, or skills that already work well."
metadata:
  {
    "deneb":
      {
        "emoji": "🧬",
        "tags": ["evolution", "optimization", "improvement", "GEPA", "autoresearch", "SkillOpt", "Self-Harness", "failure-clustering", "rejected-edit-buffer", "held-out-replay", "self-correction-queue"],
        "related_skills": ["evolution-proposal", "skill-factory", "skill-creator"],
      },
  }
---

# Skill Evolution — Self-Improving Skills

Inspired by hermes-agent's GEPA (Genetic-Pareto Prompt Evolution): analyze execution traces, understand WHY things fail, propose targeted improvements.

Also follows the SkillOpt stability pattern: treat the skill body as trainable
text state, propose bounded add/delete/replace edits, accept only after a
validation gate, and keep rejected edits as future optimizer input.

Also follows the Self-Harness pattern: mine recurrent failure mechanisms,
propose bounded edits to declared skill surfaces, and promote only candidates
that clear regression gates.

## When to Use

- A skill is **frequently loaded but produces poor results**
- User reports that a skill's instructions are **outdated or wrong**
- A skill's procedure has **known failure modes** that could be documented
- Systematic **quality sweep** across all skills is requested

## Evolution Methodology

### Phase 1: Diagnose

Before modifying anything, understand the current state:

1. **Read the skill** — full SKILL.md content
2. **Check session history** — how has this skill been used? (use session-logs)
3. **Identify failure patterns** — group failures by terminal cause, causal
   status, and reusable agent mechanism; do not treat raw error strings as
   independent anecdotes.
4. **Check related skills** — are there overlaps or conflicts?

Ask: "What SPECIFIC problem am I solving? What would success look like?"

### Phase 2: Hypothesize

Form a single, testable hypothesis:

- "Adding a 'Pitfalls' section about X will prevent error Y"
- "Rewriting the procedure step 3 to use command Z instead of W will improve reliability"
- "Adding tags ['keyword'] will improve discoverability for use case U"

Rules from autoresearch methodology:
- **One change at a time** — never bundle unrelated modifications
- **Small changes > large rewrites** — 2-line improvement that works > 50-line rewrite that doesn't
- **Reversible** — every change must be cleanly revertable
- **Textual learning rate** — prefer a bounded edit budget; don't rewrite a whole skill when one section or warning is enough
- **Rejected-edit awareness** — check `skill_lifecycle` status for `rejectedEdits` and avoid repeating candidates that already failed validation
- **Held-out replay awareness** — check `validationCases`; the evolver also injects recent cases into candidate-generation prompts, so do not remove actions, session-extracted tool calls, command fragments, fixture observations, or ordering that a replay case requires
- **Self-Harness evidence discipline** — a candidate should map one supported,
  addressable failure mechanism to one editable surface (`Procedure`,
  `Pitfalls`, `Verification`, metadata/tags). If evidence is weak or not
  addressable by the skill body, skip instead of speculating.
- **Background evidence threshold** — automatic/background evolution should wait
  for repeated real failures with recent error evidence; a fresh review finding
  can still justify an immediate evolve because it carries session-level
  evidence.
- **Deferred self-correction awareness** — check `selfCorrectionCandidates`; treat them as unapplied hypotheses for batch review, not as permission to mutate immediately

### Phase 3: Mutate

Apply the hypothesis to the SKILL.md:

```bash
# Save baseline
cp skills/<category>/<name>/SKILL.md /tmp/skill-baseline.md

# Apply change
# (edit the specific section)

# Test via autoresearch iterate
scripts/dev/iterate.sh --metric "scripts/dev/quality-metric.sh"
```

### Phase 4: Evaluate

**Constraint gates** (adapted from hermes self-evolution and SkillOpt):

| Gate | Criterion |
|---|---|
| Size | SKILL.md must stay under 15KB |
| Semantic fidelity | Original intent must be preserved |
| Cache preservation | Changes must not alter the frontmatter structure |
| No gaming | Don't optimize for the metric at the expense of real quality |
| Validation gate | Candidate changes must pass self-test and held-out replay cases before they are committed |
| Rejected buffer | Failed candidates are recorded and should inform the next attempt |
| Deferred queue | Plausible but unvalidated ideas should be recorded with `skill_lifecycle` action `self_correction`, then reviewed in batch |
| Replay regression | A candidate must not drop required actions/tool calls/session-derived input fragments/observations, reorder required traces, or introduce forbidden actions/tool calls/observations from `validationCases` |
| Evidence binding | Candidate descriptions and audit fields must state the target failure mechanism, edited surface, expected behavior change, and regression risk |

**Quality metrics** (use appropriate dev-iterate preset):

| Skill type | Metric command |
|---|---|
| Chat/prompt skills | `scripts/dev/quality-metric.sh` |
| Tool-using skills | `scripts/dev/iterate.sh --metric "scripts/dev/quality-metric.sh"` |
| Format skills | `scripts/dev/iterate.sh --metric "scripts/dev/quality-metric.sh"` |

### Phase 5: Keep or Revert

- **Improved**: keep the change, bump version patch (e.g., 1.0.0 → 1.0.1)
- **No change**: keep the original; try a different hypothesis informed by `rejectedEdits`
- **Degraded**: reject immediately, record why, and do not repeat the same edit shape
- **Promising but not validated now**: leave the file untouched and queue it through `skill_lifecycle` action `self_correction` with evidence, target files, proposed change, and risk

### Phase 6: Record

After each evolution cycle, update the skill's version and add a brief comment in the SKILL.md body noting what changed and why:

```markdown
## Changelog
- v1.0.1: Added pitfall about X timeout (caused Y failures in production)
```

## Autoresearch Integration

For systematic optimization, use the autoresearch infrastructure:

```bash
# Start autoresearch targeting the skill file
scripts/dev/autoresearch.sh start \
  --target skills/<category>/<name>/SKILL.md \
  --metric quality

# Monitor progress
scripts/dev/autoresearch.sh status

# Review results
scripts/dev/ar-results.sh --suggest
```

### Hard Constraints for Autoresearch

1. **Target only the SKILL.md file** — never modify gateway code
2. **Preserve frontmatter structure** — name, version, category must not change
3. **No new dependencies** — don't add requires.bins that aren't already available
4. **Size limit 15KB** — skills should be concise instructions, not encyclopedias
5. **Semantic drift detection** — if the skill's description no longer matches its body, revert
6. **Strict acceptance** — never land an evolved skill only because it looks plausible; it must clear validation

## Stuck Recovery (from autoresearch methodology)

| Consecutive failures | Action |
|---|---|
| 3 (Mild) | Switch strategy: if editing procedure, try pitfalls instead |
| 5 (Moderate) | Abandon current approach, start from the last known-good version |
| 8+ (Critical) | Restore original skill, document that this skill resists optimization |

## Batch Evolution

To evolve all skills in a category:

```bash
for skill in skills/<category>/*/SKILL.md; do
  echo "=== Evolving: $skill ==="
  # Read, diagnose, hypothesize, mutate, evaluate for each
done
```

Prioritize skills by:
1. Most frequently loaded (check session-logs)
2. Most reported failures
3. Oldest version (haven't been touched)

## Changelog
- v1.0.7: Added deferred self-correction queue handling.
- v1.0.6: Added background repeated-failure threshold and structured audit-field expectation.
- v1.0.5: Added Self-Harness failure-clustering and evidence-binding discipline for skill evolution.
- v1.0.4: Noted that recent validation cases are injected into candidate-generation prompts.
- v1.0.3: Added session-extracted validation trace awareness.
- v1.0.2: Added held-out replay validation guidance for evolved skill candidates.
- v1.0.1: Added SkillOpt-style validation gate and rejected-edit buffer guidance.
