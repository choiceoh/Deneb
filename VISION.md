## Deneb Vision

Deneb is a self-hosted AI agent that actually does things.
It runs on your DGX Spark, via Telegram, with your rules.

This document explains the current state and direction of the project.
We are still early, so iteration is fast.
Project overview and developer docs: [`README.md`](README.md)
Contribution guide: [`CONTRIBUTING.md`](CONTRIBUTING.md)

Deneb is a self-hosted AI agent framework built from the ground up around **lossless memory**.
The core idea: AI agents should never lose context, no matter how long the conversation runs.

The goal: a personal AI assistant that remembers everything, runs locally on DGX Spark, communicates via Telegram, and respects privacy and security.

Deneb follows deliberate design constraints documented in
[design philosophy](/concepts/design-philosophy): single-user premise,
depth over breadth, and opinionated defaults.

The current focus is:

Priority:

- **Aurora Context Engine** — DAG-based compaction, background observer, multi-layer recall
- **Vega memory backend** — Native integration for structured memory persistence
- **Aurora Memory Module** — AI-agent-first memory file management
- **Security and safe defaults**
- **Bug fixes and stability**

Next priorities:

- Supporting all major model providers (cloud, self-hosted, enterprise)
- Telegram channel hardening (the primary battle-tested channel)
- Performance and test infrastructure
- Multi-agent orchestration improvements
- Better computer-use and agent harness capabilities
- Ergonomics across CLI and Telegram

Contribution rules:

- One PR = one issue/topic. Do not bundle multiple unrelated fixes/features.
- PRs over ~5,000 changed lines are reviewed only in exceptional circumstances.
- Do not open large batches of tiny PRs at once; each PR has review cost.
- For very small related fixes, grouping into one focused PR is encouraged.

## Security

Security in Deneb is a deliberate tradeoff: strong defaults without killing capability.
The goal is to stay powerful for real work while making risky paths explicit and operator-controlled.

Canonical security policy and reporting:

- [`SECURITY.md`](SECURITY.md)

We prioritize secure defaults, but also expose clear knobs for trusted high-power workflows.

## Plugins & Memory

Deneb has an extensible skill and plugin system.
Core stays lean; optional capability should usually ship as skills or plugins.

If you build a skill, host and maintain it in your own repository.
The bar for adding skills or plugins to core is intentionally high.
Plugin docs: [`docs/tools/plugin.md`](docs/tools/plugin.md)

Memory is a special plugin slot where only one memory plugin can be active at a time.
The recommended memory stack is: **Vega memory backend + Aurora Memory Module + Aurora context engine**.

### Skills

We ship some bundled skills for baseline UX.
New skills should generally be contributed as external packages, not added to core by default.
Core skill additions should be rare and require a strong product or security reason.

### MCP Support

Deneb supports MCP (Model Context Protocol) for tool integration.

This keeps MCP integration flexible and decoupled from core runtime:

- add or change MCP servers without restarting the gateway
- keep core tool/context surface lean
- reduce MCP churn impact on core stability and security

### Setup

Deneb is currently terminal-first by design.
This keeps setup explicit: users see docs, auth, permissions, and security posture up front.

Long term, we want easier onboarding flows as hardening matures.
We do not want convenience wrappers that hide critical security decisions from users.

### Why Go + Rust?

Deneb is built as a two-language system: Go for the gateway (orchestration, networking, session management) and Rust for the core library (protocol validation, security, media, memory search, context engine).

Go was chosen for the gateway because it provides excellent concurrency, fast compilation, and a minimal dependency footprint (only 2 direct external dependencies). Rust was chosen for the core library because it provides memory safety, zero-cost FFI via CGo static linking, and SIMD-accelerated performance for search and validation operations.

## What We Will Not Merge (For Now)

- Full-doc translation sets for all docs (deferred; we plan AI-generated translations later)
- Commercial service integrations that do not clearly fit the model-provider category
- Wrapper channels around already supported channels without a clear capability or security gap
- Agent-hierarchy frameworks (manager-of-managers / nested planner trees) as a default architecture
- Heavy orchestration layers that duplicate existing agent and tool infrastructure

This list is a roadmap guardrail, not a law of physics.
Strong user demand and strong technical rationale can change it.
