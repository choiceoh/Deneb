# Skills Module

User-facing skill plugins loaded by the gateway at runtime.

## Structure

Skills support both flat and nested category layouts:

```
skills/
  <skill-name>/
    SKILL.md              # Flat layout
  <category>/
    <skill-name>/
      SKILL.md            # Nested category layout
```

## Categories

| Category | Skills |
|---|---|
| `coding` | coding-agent, github, skill-creator |
| `productivity` | gog, morning-letter, nano-pdf, session-logs, summarize |
| `devops` | healthcheck, tmux |
| `integration` | mcporter, weather, xurl |

## SKILL.md Frontmatter Standard

Every `SKILL.md` must have a YAML frontmatter block with these fields:

```yaml
---
name: my-skill                    # REQUIRED: Skill identifier
version: "1.0.0"                  # REQUIRED: Semver version
category: coding                  # REQUIRED: One of coding/productivity/devops/integration
description: "What this skill does and when to use it."  # REQUIRED
homepage: https://example.com     # OPTIONAL: Docs/homepage URL
metadata:                         # OPTIONAL: Tool requirements and install specs
  { "deneb": { "emoji": "🔧", "requires": { "bins": ["tool"] } } }
user-invocable: true              # OPTIONAL: Allow slash-command invocation (default: true)
disable-model-invocation: false   # OPTIONAL: Hide from LLM (default: false)
---
```

### Progressive Loading

The gateway uses 3-stage progressive disclosure for skills:

1. **Stage 1 (discovery):** Only the frontmatter block is parsed for metadata (name, version, category, description, requirements). The body is not loaded.
2. **Stage 2 (system prompt):** Name, category, description, and file path are injected into the LLM system prompt.
3. **Stage 3 (on-demand):** The LLM reads the full SKILL.md body via file read when it needs the skill's workflow instructions.

### Metadata Object

The `metadata` field contains a JSON object under the `deneb` key:

| Field | Type | Description |
|---|---|---|
| `emoji` | string | Visual identifier |
| `requires.bins` | string[] | All binaries must be available |
| `requires.anyBins` | string[] | At least one binary must be available |
| `requires.env` | string[] | All env vars must be set |
| `tags` | string[] | Searchable tags for skill discovery |
| `install` | object[] | Installation specs (brew/node/go/uv/download) |

## Adding a New Skill

1. Create `skills/<name>/SKILL.md` (or `skills/<category>/<name>/SKILL.md` for nested layout)
2. Add the standard frontmatter with name, version, category, description
3. Write workflow instructions, tool schemas, and safety guidelines in the body
4. The gateway discovers and loads skills automatically via `gateway-go/internal/skills/`
