# Claude Code Source Leak Analysis (2026-03-31)

> **Internal analysis only. Do not distribute.**

## Background

On March 31, 2026, Anthropic's Claude Code v2.1.88 npm package included a 59.8MB source map
file (`.map`) that exposed the entire 512,000-line TypeScript codebase (1,900 files).
Discovered by Chaofan Shou (@Fried_rice) at 4:23 AM ET.

## Documents

| File | Description |
|------|-------------|
| `01-architecture.md` | Overall architecture, directory structure, module map |
| `02-system-prompts.md` | System prompt structure, 110+ prompt fragments, token budgets |
| `03-tools-and-subagents.md` | 40+ tools, subagent system, coordinator mode |
| `04-memory-and-context.md` | autoDream, session memory, context management, caching |
| `05-feature-flags.md` | 44 feature flags, KAIROS, ULTRAPLAN, Buddy, Undercover |
| `06-security-and-permissions.md` | Permission model, anti-distillation, risk classification |
| `07-deneb-adoption-plan.md` | Techniques to adopt in Deneb, prioritized |

## Sources

- [Kuberwastaken/claude-code](https://github.com/Kuberwastaken/claude-code) — Architecture breakdown
- [Piebald-AI/claude-code-system-prompts](https://github.com/Piebald-AI/claude-code-system-prompts) — 110+ prompt inventory
- [Alex Kim analysis](https://alex000kim.com/posts/2026-03-31-claude-code-source-leak/)
- [Sebastian Raschka analysis](https://sebastianraschka.com/blog/2026/claude-code-secret-sauce.html)
- [VentureBeat coverage](https://venturebeat.com/technology/claude-codes-source-code-appears-to-have-leaked-heres-what-we-know)
- [HN discussion](https://news.ycombinator.com/item?id=47586778)
