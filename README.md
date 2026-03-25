<h1 align="center">Deneb</h1>

<p align="center">
  <strong>Self-Hosted AI Agent with Lossless Memory</strong><br>
  <a href="https://github.com/choiceoh/Deneb/releases"><img src="https://img.shields.io/badge/version-3.7.0-blue" alt="Version"></a>
  <a href="https://github.com/choiceoh/Deneb/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.x-3178c6" alt="TypeScript"></a>
  <a href="https://nodejs.org/"><img src="https://img.shields.io/badge/Node.js-22.16+-339933" alt="Node.js"></a>
</p>

---

**Deneb** is a self-hosted AI agent framework focused on one thing: **never losing context**. Deneb is a ~846K-line server engine with a custom Aurora context engine — DAG-based compaction, proactive background summarization, and full memory recall across sessions.

**Memory-first, local-first, lean-first.**

## ⭐ Why Deneb

Most AI agent frameworks hit the same wall: when conversations grow long, context gets compressed and important details vanish. Deneb solves this with **lossless memory** — every decision, number, name, and technical detail is preserved through intelligent compaction and multi-layer recall.

### What's Different

|                        | Typical Agent Framework  | Deneb                                          |
| ---------------------- | ------------------------ | ---------------------------------------------- |
| **Long conversations** | Summarize → lose details | DAG-based compaction preserves everything      |
| **Context recall**     | Vector search only       | Semantic search + DAG expansion + memory files |
| **Compaction latency** | Blocks on LLM call       | Background observer pre-computes summaries     |
| **Memory persistence** | Session-scoped           | Workspace files + JSONL transcripts + Aurora DAG  |
| **Codebase size**      | 500K–1M+ lines           | ~846K lines — auditable               |
| **Local LLM**          | Optional                 | First-class: SGLang, Ollama, vLLM support      |

### Intentional Simplification

We deliberately support fewer channels and architectures — **a smaller surface lets us move faster and ship fewer bugs.**

Rather than spreading thin across many channels and a broad user base, we focus on delivering the best possible experience to a focused group of users. This means:

- **One channel done right** (Telegram) > eight channels done halfway
- **~846K lines of auditable code** > 1M+ lines nobody can fully understand
- **Every feature battle-tested in production** before landing in the repo
- **Faster iteration** — fewer platforms to test, fewer edge cases to chase

Deneb is a focused agent engine that remembers everything and runs on a single GPU.

## ⭐ Key Features

### 🧠 Aurora Context Engine

The core differentiator. A DAG-structured summary system that compresses conversations without losing details.

- **DAG-based compaction** — Summaries reference each other in a directed acyclic graph, enabling deep recall of any past detail
- **Custom identifier preservation** — Project names, amounts, dates, and technical terms survive compression intact
- **Background observer** — Proactively pre-computes summaries using local LLM, so compaction is instant when triggered
- **Multi-layer recall** — `aurora_grep` → `aurora_describe` → `aurora_expand` → `aurora_expand_query` for progressively deeper memory retrieval
- **Quality guard** — Automatic retry with conservative settings if summary quality drops

### 🤖 Autonomous Loop Engine

24/7 self-directed operation for proactive monitoring and task execution.

- **Attention system** — Priority queue for signals from channels, goals, and deadlines
- **Goal tracking** — Define, track progress, and manage deadlines
- **State persistence** — Survives restarts with corruption recovery
- **Configurable cycles** — Adjustable interval (default 5min) with rate limiting

### 🔄 Multi-Agent Orchestration

Spawn and manage sub-agents with bounded contexts for complex tasks.

- **Sub-agent sessions** — Each with independent context and lifecycle
- **Bounded execution** — Token limits, timeouts, and tool policy per agent
- **Result streaming** — Real-time progress from sub-agents to parent

### 📡 Messaging Channels

**Telegram** is the only channel with full, battle-tested production support — reactions, inline buttons, polls, topics, group policies, and all core features.

Other channel configs (Discord, Signal, Slack, WhatsApp, iMessage, Google Chat, MS Teams) exist at the schema level as stubs. These are not actively maintained — enabling them will likely encounter bugs. They remain in the codebase as config-level scaffolding for future implementation, not as working features.

We chose to ship one channel that works perfectly over eight that sort of work.

### 🧰 Tool System

| Tool       | Description                                  |
| ---------- | -------------------------------------------- |
| File I/O   | Read, write, edit workspace files            |
| Web Search | Perplexity, Brave Search, auto-detect        |
| Browser    | Playwright automation and scraping           |
| PDF        | Native PDF analysis with vision models       |
| Image      | Multi-image analysis with vision models      |
| Memory     | Semantic search + Aurora expansion            |
| Cron       | Scheduled tasks, heartbeats, morning letters |
| Sub-agents | Spawn bounded sub-agents for complex tasks   |
| MCP        | Model Context Protocol tool integration      |

### 🤖 Model Providers

**Cloud:** Anthropic · OpenAI · Google (Gemini) · Mistral · xAI (Grok) · Z.AI (GLM) · OpenRouter · Perplexity · Together AI · DeepSeek

**Self-Hosted:** Ollama · SGLang · vLLM · LiteLLM

**Enterprise:** AWS Bedrock · Google Vertex AI · Azure OpenAI

## 🚀 Quick Start

### Prerequisites

- Node.js 22.16+ (Node 24 recommended)
- An LLM API key (or a local model server)

### Install

```bash
git clone https://github.com/choiceoh/Deneb.git
cd Deneb
pnpm install
pnpm build
```

### Configure

```bash
# Interactive setup wizard
node deneb.mjs onboard
```

Or edit `~/.deneb/deneb.json` directly.

### Run

```bash
# Start the gateway daemon
node deneb.mjs gateway
```

## 📁 Architecture

```
Deneb/                          (~846K lines TypeScript)
├── src/
│   ├── context-engine/aurora/   # Aurora engine — compaction, observer, DAG
│   ├── autonomous/             # Autonomous loop engine
│   ├── agents/                 # Agent loop, sessions, compaction
│   ├── cron/                   # Scheduled tasks & heartbeat
│   ├── memory/                 # Semantic memory search
│   ├── channels/               # Channel plugin framework
│   ├── config/                 # Config schema & validation
│   ├── gateway/                # Gateway server & daemon
│   ├── plugin-sdk/             # Plugin SDK
│   ├── secrets/                # Credential management
│   ├── infra/outbound/         # Message delivery & routing
│   ├── routing/                # Message routing
│   ├── tts/                    # Text-to-speech
│   └── web-search/             # Web search integration
├── extensions/                 # Channel extensions (Telegram)
├── vega/                       # Vega memory backend
└── ui/                         # Web dashboard
```

## 🛠️ Development

```bash
pnpm install
pnpm build
pnpm dev        # Development mode
pnpm test       # Run tests (vitest)
pnpm check      # Lint + format check (oxlint + oxfmt)
pnpm format:fix # Auto-format (oxfmt --write)
```

## 📄 License

MIT

---

