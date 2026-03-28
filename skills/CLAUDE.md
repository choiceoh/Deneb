# Skills Module

User-facing skill plugins loaded by the gateway at runtime.

## Structure

Each skill is a directory containing a `SKILL.md` file:

```
skills/
  <skill-name>/
    SKILL.md      # Workflow definition, tool schemas, usage patterns
```

## Available Skills

`coding-agent`, `github`, `gog`, `healthcheck`, `mcporter`, `morning-letter`, `nano-pdf`, `session-logs`, `skill-creator`, `summarize`, `tmux`, `weather`, `xurl`

## Adding a New Skill

1. Create `skills/<name>/SKILL.md` with:
   - Skill name and description
   - Trigger patterns (when the skill activates)
   - Tool schemas (JSON Schema format, if the skill provides tools)
   - Workflow steps
   - Safety guidelines
2. The gateway loads skills via `gateway-go/internal/chat/system_prompt.go`
3. Skill content is injected into the system prompt for the LLM

## Skill File Format

Each `SKILL.md` defines:
- **Name & trigger:** When this skill should activate
- **Tools:** Available tool signatures with JSON schemas
- **Workflow:** Step-by-step instructions for the agent
- **Safety:** Guardrails and constraints
