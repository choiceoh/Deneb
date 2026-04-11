# Skills Module

User-facing skill plugins loaded by the gateway at runtime.

## Structure

Skills use nested category layout (inspired by [hermes-agent](https://github.com/NousResearch/hermes-agent)):

```
skills/
  CLAUDE.md
  <category>/
    DESCRIPTION.md            # Category description (YAML frontmatter)
    <skill-name>/
      SKILL.md                # Skill definition (required)
      scripts/                # Optional helper scripts
      references/             # Optional reference docs
```

Each category directory contains a `DESCRIPTION.md` with YAML frontmatter:

```yaml
---
description: One-line description of what skills belong in this category.
---
```

## Categories

| Category | Description | Skills |
|---|---|---|
| `coding` | Software development, code generation, version control, CI/CD | autoresearch, github, skill-creator, skill-evolution, skill-factory |
| `productivity` | Daily workflows, documents, summarization, personal automation | morning-letter, session-logs, summarize |
| `devops` | System monitoring, terminal management, infrastructure | healthcheck, tmux |
| `integration` | External service connectivity, API bridges | mcporter |

## Skill vs Tool Decision

Adapted from hermes-agent's framework:

**Make it a Skill when:**
- The capability can be expressed as instructions + shell commands + existing tools
- It wraps external CLIs or APIs callable via terminal
- No custom Go integration or persistent state management needed
- Examples: arXiv search, git workflows, Docker management, PDF processing

**Make it a Tool when:**
- Requires end-to-end integration with auth flows managed by the gateway
- Needs custom processing logic that must execute precisely every time
- Handles binary data, streaming, or real-time events
- Requires persistent state or in-process memory
- Examples: browser automation, TTS, Vega search, memory operations

## SKILL.md Frontmatter Standard

Every `SKILL.md` must have a YAML frontmatter block:

```yaml
---
name: my-skill                    # REQUIRED: Skill identifier (lowercase, hyphens)
version: "1.0.0"                  # REQUIRED: Semver version
category: coding                  # REQUIRED: Must match parent directory name
description: "What this does."    # REQUIRED: When to use + NOT for
homepage: https://example.com     # OPTIONAL: Docs/homepage URL
metadata:                         # OPTIONAL: Tool requirements and behavior
  {
    "deneb":
      {
        "emoji": "🔧",
        "requires": { "bins": ["tool"] },
        "tags": ["keyword1", "keyword2"],
        "related_skills": ["other-skill"],
        "install": [...],
      },
  }
user-invocable: true              # OPTIONAL: Allow /skill-name commands (default: true)
disable-model-invocation: false   # OPTIONAL: Hide from LLM (default: false)
---
```

### Metadata Object

The `metadata` field contains a JSON object under the `deneb` key:

| Field | Type | Description |
|---|---|---|
| `emoji` | string | Visual identifier for UI |
| `requires.bins` | string[] | All listed binaries must be available |
| `requires.anyBins` | string[] | At least one listed binary must be available |
| `requires.env` | string[] | All listed env vars must be set |
| `tags` | string[] | Searchable keywords for skill discovery |
| `related_skills` | string[] | Cross-references to complementary skills |
| `requires_tools` | string[] | Show only when ALL listed agent tools are available |
| `fallback_for_tools` | string[] | Show only when ANY listed tool is UNavailable (fallback) |
| `install` | object[] | Installation specs (brew/node/go/uv/apt/download) |

### Description Field Convention

Use the pattern: `"What it does. Use when: (triggers). NOT for: (anti-patterns)."`

This helps the LLM decide when to load the skill and when to skip it.

## SKILL.md Body Structure

Recommended sections (adapted from hermes-agent):

| Section | Purpose |
|---|---|
| `# Title` | Skill name and one-line summary |
| `## When to Use` | Trigger conditions and use cases |
| `## Quick Reference` | Common commands, API calls, or patterns |
| `## Procedure` | Step-by-step workflow instructions |
| `## Pitfalls` | Known failure modes, edge cases, workarounds |
| `## Verification` | How to confirm the skill's output is correct |

Not all sections are required for every skill. Use what makes sense for the complexity.

## Progressive Loading

The gateway uses 3-stage progressive disclosure to minimize token usage:

1. **Stage 1 (discovery):** Only the frontmatter block is parsed for metadata (name, version, category, description, requirements). The body is not loaded.
2. **Stage 2 (system prompt):** Name, category, description, and file path are injected into the LLM system prompt.
3. **Stage 3 (on-demand):** The LLM reads the full SKILL.md body via file read when it needs the skill's workflow instructions.

## Adding a New Skill

1. Pick the right category directory (or create a new one with `DESCRIPTION.md`)
2. Create `skills/<category>/<name>/SKILL.md`
3. Add standard frontmatter with name, version, category, description
4. Add `tags` and `related_skills` in metadata for discoverability
5. Write body following the recommended section structure
6. The gateway discovers and loads skills automatically via `gateway-go/internal/domain/skills/`

## Adding a New Category

1. Create `skills/<category>/DESCRIPTION.md` with a one-line YAML description
2. Add the category to the table above
3. Skills in the directory will auto-discover with the directory name as default category
4. Frontmatter `category` field overrides the directory-based category if set

## Conditional Activation

Skills can show/hide based on available agent tools (hermes-agent pattern):

```yaml
metadata:
  {
    "deneb":
      {
        "requires_tools": ["exec", "terminal"],
        "fallback_for_tools": ["web_search"],
      },
  }
```

| Field | Semantics |
|---|---|
| `requires_tools` | Skill visible **only when ALL** listed tools are registered |
| `fallback_for_tools` | Skill visible **only when ANY** listed tool is UNavailable |

Use `fallback_for_tools` for free/CLI alternatives to premium tools.
Use `requires_tools` for skills that depend on specific agent capabilities.

## Skill Lifecycle (Hermes-Agent Patterns)

The skill system supports a closed learning loop:

```
Experience → Factory → Creation → Use → Evolution → Improved Skill
```

| Phase | Skill | Description |
|---|---|---|
| **Creation** | `skill-factory` | Extract reusable patterns from complex workflows |
| **Authoring** | `skill-creator` | Create/edit/audit SKILL.md files |
| **Evolution** | `skill-evolution` | Optimize skills via autoresearch methodology |

### Autonomous Skill Creation

After completing complex multi-step tasks (5+ tool calls), the agent should consider:
> "이 작업은 스킬로 만들 가치가 있는가?"

If the pattern is reusable, offer to create a skill using `skill-factory`.

### Self-Evolution

Skills improve over time via the autoresearch loop:
1. Diagnose failure patterns in skill usage
2. Form a single hypothesis for improvement
3. Mutate the SKILL.md (one atomic change)
4. Evaluate via `scripts/dev/iterate.sh`
5. Keep improvements, revert failures

## Prompt Cache Design

The skills prompt is placed in the **semi-static block** of the system prompt:

| Block | Content | Cache behavior |
|---|---|---|
| Static | Identity, tooling, communication | Cached across all turns |
| **Semi-static** | **Skills catalog (XML)** | **Cached until SKILL.md changes** |
| Dynamic | Memory, context, datetime | Rebuilt every turn |

Skills are rendered in stable sorted order to maximize Anthropic prompt cache hits.
Changes to SKILL.md files invalidate the skills cache via filesystem watcher.
