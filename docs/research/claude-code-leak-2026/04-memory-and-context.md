# Claude Code Memory & Context Management

## autoDream — Memory Consolidation Engine

Location: `src-rust/crates/services/autoDream/`

### Concept

A background memory consolidation process that runs like a "dream":
> "You are performing a dream — synthesize what you've learned recently
> into durable memories."

### Trigger System (3 Gates)

All three gates must pass before consolidation runs:

1. **Time Gate**: 24 hours since last consolidation
2. **Session Gate**: 5+ sessions completed since last consolidation
3. **Lock Gate**: No other consolidation currently running

### 4-Phase Process

```
Phase 1: Orient
  └── Read current MEMORY.md to understand existing memories

Phase 2: Gather
  └── Collect recent signals:
      - Daily logs
      - Drifted memories (stale or outdated)
      - Transcript search for recent context

Phase 3: Consolidate
  └── Write new memories:
      - Convert relative dates → absolute dates
      - Delete contradictions
      - Merge related memories
      - Synthesize patterns

Phase 4: Prune
  └── Enforce limits:
      - < 200 lines
      - ~ 25KB max
      - Remove lowest-value entries
```

### Safety

- **Read-only bash access**: autoDream cannot modify project files
- Only operates on memory files
- Consolidation lock prevents concurrent runs

---

## Session Memory Structure

Each session maintains structured markdown with sections:

```markdown
# Session: [title]

## Current State
[What the agent is currently doing]

## Task
[Original task specification]

## Files and Functions
[Key files and functions involved]

## Workflow
[Steps taken so far]

## Errors & Corrections
[Mistakes made and how they were fixed]

## Codebase Documentation
[Discovered patterns, architecture notes]

## Learnings
[Insights gained during this session]

## Key Results
[Outcomes and deliverables]

## Worklog
[Timestamped activity log]
```

---

## Context Management

### Repository Context Loading

On session start, the system loads:
- Main git branch name
- Current git branch name
- Recent commit history
- `CLAUDE.md` for project instructions
- Workspace context files

### Context File Hierarchy

Files searched in order (workspace → ancestors, max 10 levels up to home dir):
1. `CLAUDE.md` — Project rules/conventions
2. Other context files as configured

### Overflow Handling

When tool results exceed size limits:
1. Full result written to disk at temporary path
2. Context receives only: preview text + file reference path
3. Agent can read the full file later if needed

### File-Read Deduplication

- Tracks files read within a session
- If same file requested again with no modification, returns cached result
- Reduces redundant token consumption across multiple reads

### Autocompaction (Summarization)

When context grows too long:
1. Detect context approaching limit
2. Summarize older conversation turns
3. Replace full turns with summaries
4. Preserve critical information (task, files, errors)

Uses a dedicated "conversation summarization" utility agent prompt.

---

## Prompt Cache Architecture

### Anthropic API Cache Optimization

Three-tier cache strategy:

```
Tier 1: Static (globally cached)
  - Identity, behavioral rules
  - CYBER_RISK_INSTRUCTION
  - Tool definitions & JSON schemas
  → cache_control: { type: "ephemeral" }

Tier 2: Semi-static (cached per tool set)
  - Available skills/capabilities
  - Mode-specific instructions
  → cache_control: { type: "ephemeral" }

Tier 3: Dynamic (never cached)
  - User context, session state
  - Memory content
  - Workspace info, timestamps
  → No cache marker
```

### Dynamic Boundary Marker

```
SYSTEM_PROMPT_DYNAMIC_BOUNDARY
```

Everything above: cached. Everything below: regenerated per request.

### Tool Schema Cache

Tool JSON schemas serialized once and cached separately from prompt text.
Reduces serialization cost on every request.

### Intentional Cache Busting

`DANGEROUS_uncachedSystemPromptSection()` — marks content that MUST NOT
be cached, even if it appears in the static section. Used for volatile
content that changes between requests but needs to appear early in the prompt.

---

## MEMORY.md Management

### Structure

Main index file with dated topic sub-files:

```
~/.claude/
├── MEMORY.md          # Main index (< 200 lines, ~25KB)
├── memory/
│   ├── 2026-03-15-project-architecture.md
│   ├── 2026-03-20-user-preferences.md
│   └── 2026-03-28-debugging-patterns.md
```

### Operations

- **memory search**: Full-text search across all memory files
- **memory get**: Read specific memory entry
- **memory set**: Write/update memory entry
- **memory forget**: Remove specific memory

### Pruning Rules

- Total MEMORY.md must stay under 200 lines / ~25KB
- autoDream consolidates and prunes automatically
- Contradictions resolved in favor of newer information
- Relative dates converted to absolute for durability
