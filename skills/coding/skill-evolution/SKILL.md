---
name: skill-evolution
version: "1.0.0"
category: coding
description: "Evolve and optimize existing skills using autoresearch methodology. Use when: (1) a skill produces suboptimal results, (2) a skill's instructions are outdated or incomplete, (3) systematic improvement of skill quality is requested. NOT for: creating new skills (use skill-factory), cosmetic changes, or skills that already work well."
metadata:
  {
    "deneb":
      {
        "emoji": "🧬",
        "tags": ["evolution", "optimization", "improvement", "GEPA", "autoresearch"],
        "related_skills": ["autoresearch", "skill-factory", "skill-creator"],
      },
  }
---

# Skill Evolution — Self-Improving Skills

Inspired by hermes-agent's GEPA (Genetic-Pareto Prompt Evolution): analyze execution traces, understand WHY things fail, propose targeted improvements.

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
3. **Identify failure patterns** — what goes wrong and why?
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

### Phase 3: Mutate

Apply the hypothesis to the SKILL.md:

```bash
# Save baseline
cp skills/<category>/<name>/SKILL.md /tmp/skill-baseline.md

# Apply change
# (edit the specific section)

# Test via autoresearch iterate
scripts/dev-iterate.sh --metric "scripts/dev-quality-metric.sh"
```

### Phase 4: Evaluate

**Constraint gates** (adapted from hermes self-evolution):

| Gate | Criterion |
|---|---|
| Size | SKILL.md must stay under 15KB |
| Semantic fidelity | Original intent must be preserved |
| Cache preservation | Changes must not alter the frontmatter structure |
| No gaming | Don't optimize for the metric at the expense of real quality |

**Quality metrics** (use appropriate dev-iterate preset):

| Skill type | Metric command |
|---|---|
| Chat/prompt skills | `scripts/dev-quality-metric.sh` |
| Tool-using skills | `scripts/dev-iterate.sh --metric "scripts/dev-quality-metric.sh"` |
| Format skills | `scripts/dev-iterate.sh --metric "scripts/dev-quality-metric.sh"` |

### Phase 5: Keep or Revert

- **Improved**: keep the change, bump version patch (e.g., 1.0.0 → 1.0.1)
- **No change**: revert entirely, try a different hypothesis
- **Degraded**: revert immediately, document what didn't work

### Phase 6: Record

After each evolution cycle, update the skill's version and add a brief comment in the SKILL.md body noting what changed and why:

```markdown
## Changelog
- v1.0.1: Added pitfall about X timeout (caused Y failures in production)
```

## Autoresearch Integration

For systematic optimization, use the autoresearch infrastructure:

```bash
# Generate a quality metric for the target skill
scripts/dev-metric-gen.sh quality

# Start autoresearch targeting the skill file
scripts/dev-autoresearch.sh start \
  --target skills/<category>/<name>/SKILL.md \
  --metric quality

# Monitor progress
scripts/dev-autoresearch.sh status

# Review results
scripts/dev-ar-results.sh --suggest
```

### Hard Constraints for Autoresearch

1. **Target only the SKILL.md file** — never modify gateway code
2. **Preserve frontmatter structure** — name, version, category must not change
3. **No new dependencies** — don't add requires.bins that aren't already available
4. **Size limit 15KB** — skills should be concise instructions, not encyclopedias
5. **Semantic drift detection** — if the skill's description no longer matches its body, revert

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
