<p align="center">
  <img src="https://raw.githubusercontent.com/choiceoh/Deneb/master/ui/public/favicon.svg" alt="⭐ Deneb" width="120" height="120">
</p>

<h1 align="center">Deneb</h1>

<p align="center">
  <strong>Self-Hosted AI Agent with Lossless Memory</strong><br>
  <a href="https://github.com/choiceoh/Deneb/releases"><img src="https://img.shields.io/badge/version-3.5-blue" alt="Version"></a>
  <a href="https://github.com/choiceoh/Deneb/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.x-3178c6" alt="TypeScript"></a>
  <a href="https://nodejs.org/"><img src="https://img.shields.io/badge/Node.js-22+-339933" alt="Node.js"></a>
</p>

---

**Deneb** is a self-hosted AI agent framework focused on one thing: **never losing context**. Built on top of OpenClaw, Deneb strips away the mobile apps and trims the codebase to a lean 230K-line server engine — while adding a custom Lossless Context Management (LCM) system with DAG-based compaction, proactive background summarization, and full memory recall across sessions.

## ⭐ Why Deneb

Most AI agent frameworks hit the same wall: when conversations grow long, context gets compressed and important details vanish. Deneb solves this with **lossless memory** — every decision, number, name, and technical detail is preserved through intelligent compaction and multi-layer recall.

### What's Different

|                        | Typical Agent Framework  | Deneb                                          |
| ---------------------- | ------------------------ | ---------------------------------------------- |
| **Long conversations** | Summarize → lose details | DAG-based compaction preserves everything      |
| **Context recall**     | Vector search only       | Semantic search + DAG expansion + memory files |
| **Compaction latency** | Blocks on LLM call       | Background observer pre-computes summaries     |
| **Memory persistence** | Session-scoped           | Workspace files + JSONL transcripts + LCM DAG  |
| **Codebase size**      | 500K–1M+ lines           | 230K lines — lean and auditable                |
| **Local LLM**          | Optional                 | First-class: SGLang, Ollama, vLLM support      |

### What We Removed from OpenClaw

Deneb is a focused fork of [OpenClaw](https://github.com/openclaw/openclaw). We removed:

- Mobile apps (iOS/Android/macOS companion apps — Swift + Kotlin)
- Desktop companion app
- Unused channel implementations (Nostr, Twitch, Tlon, Zalo, etc.)
- Enterprise plugins not needed for single-instance deployment

The result: **110K lines → 230K lines of pure server-side agent engine**, with every feature battle-tested in production.

## ⭐ Key Features

### 🧠 Lossless Context Management (LCM)

The core differentiator. A DAG-structured summary system that compresses conversations without losing details.

- **DAG-based compaction** — Summaries reference each other in a directed acyclic graph, enabling deep recall of any past detail
- **Custom identifier preservation** — Project names, amounts, dates, and technical terms survive compression intact
- **Background observer** — Proactively pre-computes summaries using local LLM, so compaction is instant when triggered
- **Multi-layer recall** — `lcm_grep` → `lcm_describe` → `lcm_expand` → `lcm_expand_query` for progressively deeper memory retrieval
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

Battle-tested in production with the channels that matter most.

| Channel         | Notes                                                               |
| --------------- | ------------------------------------------------------------------- |
| **Telegram**    | ✅ Primary — full feature support, reactions, polls, inline buttons |
| **Discord**     | ✅ Bot Gateway                                                      |
| **Signal**      | ✅ signal-cli integration                                           |
| **WhatsApp**    | ✅ WhatsApp Bridge                                                  |
| **Slack**       | ✅ Socket Mode                                                      |
| **iMessage**    | ✅ BlueBubbles                                                      |
| **Google Chat** | ✅ Service Account                                                  |
| **MS Teams**    | ✅ Bot Framework                                                    |

Additional channel schemas exist in the config layer (IRC, Matrix, LINE, Feishu, Mattermost, Nostr, Twitch) — these are inherited from OpenClaw and may require community extensions.

### 🧰 Tool System

| Tool       | Description                                  |
| ---------- | -------------------------------------------- |
| File I/O   | Read, write, edit workspace files            |
| Web Search | Perplexity, Brave Search, auto-detect        |
| Browser    | Playwright automation and scraping           |
| PDF        | Native PDF analysis with vision models       |
| Image      | Multi-image analysis with vision models      |
| Memory     | Semantic search + LCM expansion              |
| Cron       | Scheduled tasks, heartbeats, morning letters |
| Sub-agents | Spawn bounded sub-agents for complex tasks   |
| MCP        | Model Context Protocol tool integration      |

### 🤖 Model Providers

**Cloud:** Anthropic · OpenAI · Google (Gemini) · Mistral · xAI (Grok) · Z.AI (GLM) · OpenRouter · Perplexity · Together AI · DeepSeek

**Self-Hosted:** Ollama · SGLang · vLLM · LiteLLM

**Enterprise:** AWS Bedrock · Google Vertex AI · Azure OpenAI

## 🚀 Quick Start

### Prerequisites

- Node.js 22+
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

Or edit `~/.openclaw/openclaw.json` directly.

### Run

```bash
# Start the gateway daemon
node deneb.mjs gateway
```

## 📁 Architecture

```
Deneb/                          (~230K lines TypeScript)
├── src/
│   ├── context-engine/lcm/     # LCM engine — compaction, observer, DAG
│   ├── autonomous/             # Autonomous loop engine
│   ├── agents/                 # Agent loop, sessions, compaction
│   ├── cron/                   # Scheduled tasks & heartbeat
│   ├── memory/                 # Semantic memory search
│   ├── channels/               # Channel plugin framework
│   ├── config/                 # Config schema & validation
│   ├── gateway/                # Gateway server & daemon
│   ├── plugins/                # Plugin SDK
│   ├── secrets/                # Credential management
│   ├── infra/outbound/         # LLM provider adapters
│   └── auto-reply/             # Message routing
├── extensions/                 # Channel extensions (Telegram, Discord, etc.)
└── ui/                         # Web dashboard
```

## 🛠️ Development

```bash
pnpm install
pnpm build
pnpm dev        # Development mode
pnpm test       # Run tests
pnpm lint       # Lint
pnpm format     # Format
```

## 📄 License

MIT — Forked from [OpenClaw](https://github.com/openclaw/openclaw) with ❤️

---

<p align="center">
  <img src="https://raw.githubusercontent.com/choiceoh/Deneb/master/ui/public/favicon.svg" alt="⭐" width="40" height="40">
</p>
