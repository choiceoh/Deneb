# Claude Code Architecture

> Source: npm source map leak v2.1.88 (2026-03-31). Internal codename: **Tengu**.

## Tech Stack

- **Language**: TypeScript (bundled with Bun)
- **Runtime**: Node.js / Bun
- **Build**: Bun bundler (source maps enabled by default — the leak cause)
- **Distribution**: npm (`@anthropic-ai/claude-code`)
- **Crate structure**: Modular Rust-inspired crate layout under `src-rust/crates/`

## Directory Structure

```
src-rust/crates/
├── cli/              # Main entry point, CLI argument parsing
├── tools/            # 40+ tool implementations
├── core/             # System prompts, constants, permissions, config
│   └── constants/    # Prompt fragments, dynamic boundary markers
├── assistant/        # KAIROS proactive mode (feature-gated)
├── services/
│   └── autoDream/    # Memory consolidation engine
├── coordinator/      # Multi-agent orchestration
├── buddy/            # Companion pet system (feature-gated)
├── api/              # API client layer (Anthropic API)
└── query/            # Query processing
```

## Core Architectural Patterns

### 1. Prompt-Based Orchestration (Not Framework-Based)

Claude Code does NOT use LangGraph, LangChain, or similar frameworks.
The entire orchestration algorithm is embedded in the system prompt itself.
A single coordinator agent dynamically spawns sub-agents on demand — too fluid
for traditional graph-based state machines.

> "The whole orchestration algorithm is a prompt, not code."
> — HN discussion

### 2. Modular Prompt Assembly

System prompt is NOT a single monolithic string. Instead, it's composed from
110+ fragments that are conditionally loaded based on:

- Environment configuration
- User settings & user type (employee vs external)
- Active feature flags
- Session context & mode (Plan, Explore, Delegate, Learning)
- Available tools

Key marker: `SYSTEM_PROMPT_DYNAMIC_BOUNDARY` splits cacheable static sections
from per-request dynamic sections.

### 3. Static/Dynamic Cache Boundary

```
[STATIC SECTION — globally cached]
  - Identity, behavioral rules
  - Tool definitions & schemas
  - Safety instructions
  ── SYSTEM_PROMPT_DYNAMIC_BOUNDARY ──
[DYNAMIC SECTION — per-request]
  - User context, session state
  - Memory, workspace info
  - Active mode instructions
```

`DANGEROUS_uncachedSystemPromptSection()` marks sections that intentionally
break the cache when volatile content is needed.

### 4. Comment-Driven Agent Memory

Inline code comments serve as persistent agent context — "free long-term agent
memory with zero infrastructure." Agents reliably read inline documentation
while potentially missing external docs that drift.

### 5. Business Context in Source

Operational metrics embedded directly in code comments:
> "BQ 2026-03-10: 1,279 sessions had 50+ consecutive failures...
>  wasting ~250K API calls/day globally."

This optimizes for agent visibility during development but inadvertently leaked
business intelligence.

## Build & Distribution

- Bun bundler produces single JS file + source map
- Source maps contain `sourcesContent` JSON array with complete original source
- npm package distributed via `@anthropic-ai/claude-code`
- Native installer (recommended) uses standalone binary, bypassing npm deps
- `.npmignore` was missing `*.map` entry — root cause of leak

## Internal Codename: Tengu

"Tengu" appears hundreds of times across:
- Feature flags (`tengu_scratch`, `tengu_amber_flint`, `tengu_penguin_mode`)
- Analytics events (`tengu_org_penguin_mode_fetch_failed`)
- API endpoints (`/api/claude_code_penguin_mode`)
- Kill switches (`tengu_penguins_off`)
