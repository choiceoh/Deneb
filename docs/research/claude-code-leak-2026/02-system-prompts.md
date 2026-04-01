# Claude Code System Prompts

> 110+ prompt fragments, conditionally assembled per request.

## Prompt Inventory Overview

Total documented prompts: **110+**
Token range: 12 tokens (minimal reminders) → 5,106 tokens (comprehensive tool refs)

## Categories

### Agent Prompts (Sub-agents)

| Prompt | Tokens | Purpose |
|--------|--------|---------|
| Explore | 494 | Fast codebase exploration subagent |
| Plan mode enhanced | 636 | Software architect for implementation plans |
| Agent creation architect | 1,110 | Designs new agent configurations |
| CLAUDE.md creation | 384 | Generates project instruction files |
| Status line setup | 1,999 | Configures IDE status line |

### Slash Command Prompts

| Command | Tokens | Purpose |
|---------|--------|---------|
| /batch | 1,106 | Batch operations |
| /pr-comments | 402 | PR comment handling |
| /review-pr | 211 | Pull request review |
| /schedule | 2,468 | Cron scheduling |
| /security-review | 2,607 | Security audit |

### Utility Agent Prompts (23 total)

- Agent hook (pre/post tool execution hooks)
- Auto mode rule reviewer
- Bash command utilities
- Coding session title generator
- **Conversation summarization** (autocompaction)
- **Dream memory consolidation** (autoDream)
- General purpose search
- Verification specialist
- Range: 78 → 2,453 tokens

### SDK/API Reference Data Prompts

| Category | Languages | Tokens |
|----------|-----------|--------|
| Agent SDK patterns | Python, TypeScript | 2,656 / 1,529 |
| Agent SDK reference | Python, TypeScript | 3,299 / 2,943 |
| Claude API reference | C#, Go, Java, PHP, Python, Ruby, TS, cURL | varies |
| Files API reference | Python, TypeScript | varies |
| Claude model catalog | — | 2,295 |
| HTTP error codes | — | 1,922 |
| Prompt caching guide | — | 1,880 |
| Live documentation sources | — | 2,336 |

### Core System Prompt Segments (60+)

| Segment | Tokens | Notes |
|---------|--------|-------|
| Advisor tool instructions | 443 | |
| Auto mode | 255 | ML-based auto-approval |
| Learning mode | 1,042 | Educational exploration mode |
| Minimal mode | 164 | Ultra-compact for simple ops |
| Fork usage guidelines | 419 | Git worktree isolation |
| Subagent delegation examples | 606 | How to properly delegate |
| Tool usage policies | varies | Separate modules per tool type |

### "Doing Tasks" Prompts (8 variants)

Each addresses a specific behavioral dimension:
1. Ambitious tasks (don't discourage complexity)
2. File minimization (prefer editing over creating)
3. Backward compatibility (avoid unnecessary shims)
4. Abstractions (don't over-abstract)
5. Error handling (don't over-validate)
6. Security (OWASP awareness)
7. Time estimates (avoid predictions)
8. Feature scope (don't gold-plate)

Token range: 47-104 per variant.

### Insights Analysis Prompts

| Prompt | Tokens | Purpose |
|--------|--------|---------|
| At-a-glance summary | 569 | Session overview |
| Friction analysis | 139 | Identify user friction points |
| Session facets extraction | 310 | Structured session metadata |
| Suggestions generation | 748 | Proactive improvement ideas |

### System Reminders (40+)

Brief contextual notifications injected during execution:
- Plan mode status (5-phase: 1,297 tokens | iterative: 936 tokens)
- File modification alerts
- Hook execution feedback
- Memory file contents
- Token usage statistics
- IDE interaction notifications
- Range: 12 → 1,297 tokens

## Key Prompt Engineering Techniques

### 1. Behavioral Differentiation by User Type

Anthropic employees (`USER_TYPE === 'ant'`) receive stricter/more honest
instructions than external users. Different behavioral guardrails apply.

### 2. Anti-Distillation Instructions

When `anti_distillation: ['fake_tools']` is enabled, the API server silently
injects decoy tool definitions into the system prompt to poison competitor
distillation attempts.

### 3. CYBER_RISK_INSTRUCTION

Owned by Safeguards team (David Forsythe, Kyla Guru):
> "DO NOT MODIFY WITHOUT SAFEGUARDS TEAM REVIEW"

Handles security-sensitive behavioral boundaries.

### 4. Undercover Mode Prompt

Active by default for external repos:
> "You are operating UNDERCOVER... Your commit messages... MUST NOT contain
> ANY Anthropic-internal information. Do not blow your cover."

Blocks: animal codenames (Capybara, Tengu), unreleased model versions
(opus-4-7, sonnet-4-8), internal repos, Slack channels, co-author attributions.
No force-off switch — "if not confident we're internal, stay undercover."

### 5. Frustration Detection

Regex-based patterns detect user frustration in messages. When detected,
the agent adjusts behavior (more careful, apologetic, focused).

### 6. Cache-Aware Prompt Design

- Static identity/rules cached globally across sessions
- Tool schemas cached separately
- Dynamic user context appended after boundary marker
- `ephemeral` cache control markers on Anthropic API content blocks
