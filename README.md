<p align="center">
  <img src="https://raw.githubusercontent.com/choiceoh/Deneb/master/ui/public/favicon.svg" alt="⭐ Deneb" width="120" height="120">
</p>

<h1 align="center">Deneb</h1>

<p align="center">
  <strong>Multi-Channel AI Gateway & Autonomous Agent Framework</strong><br>
  <a href="https://github.com/choiceoh/Deneb/releases"><img src="https://img.shields.io/badge/version-3.3-blue" alt="Version"></a>
  <a href="https://github.com/choiceoh/Deneb/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.x-3178c6" alt="TypeScript"></a>
  <a href="https://nodejs.org/"><img src="https://img.shields.io/badge/Node.js-22+-339933" alt="Node.js"></a>
</p>

---

**Deneb** is a self-hosted AI gateway that connects LLMs to your messaging channels — Telegram, Discord, Signal, Slack, WhatsApp, LINE, and 20+ more. It runs your AI agent as a persistent service with memory, tools, cron automation, multi-agent orchestration, and an autonomous loop engine for 24/7 operation.

## ⭐ Key Features

- **20+ Messaging Channels** — Telegram, Discord, Signal, Slack, WhatsApp, LINE, iMessage (BlueBubbles), Matrix, MS Teams, Google Chat, IRC, Nostr, Twitch, and more
- **Multi-Provider LLM Support** — Anthropic, OpenAI, Google, Mistral, xAI, Z.AI, OpenRouter, Perplexity, Together, Ollama, vLLM, SGLang, Bedrock, Vertex, Azure, and custom OpenAI-compatible endpoints
- **Lossless Context Management (LCM)** — DAG-based conversation compaction that preserves details across context window limits. Never lose important context.
- **Autonomous Loop Engine** — 24/7 self-directed operation with attention management, goal tracking, and cyclic task execution
- **Persistent Memory** — Session history, workspace files, and semantic memory search that survives restarts
- **Tool System** — File I/O, web search, browser automation, PDF analysis, image understanding, cron scheduling, and extensible MCP tools
- **Multi-Agent Orchestration** — Spawn, manage, and communicate between sub-agents with bounded contexts
- **Plugin Architecture** — Extensions for channels, tools, context engines, and web search providers
- **Dashboard & CLI** — Web-based control panel + full-featured command-line interface

## 🚀 Quick Start

### Prerequisites

- Node.js 22+
- An LLM API key (Anthropic, OpenAI, Google, etc.)

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

Or edit `deneb.json` directly:

```json
{
  "models": {
    "providers": {
      "anthropic": {
        "apiKey": "sk-ant-..."
      }
    }
  },
  "channels": {
    "telegram": {
      "token": "BOT_TOKEN_HERE"
    }
  }
}
```

### Run

```bash
# Start the gateway daemon
node deneb.mjs gateway

# Or in development mode
pnpm dev
```

## 📡 Supported Channels

| Channel        | Type              | Status    |
| -------------- | ----------------- | --------- |
| Telegram       | Bot API           | ✅ Stable |
| Discord        | Bot Gateway       | ✅ Stable |
| Signal         | signal-cli        | ✅ Stable |
| Slack          | Socket Mode       | ✅ Stable |
| WhatsApp       | WhatsApp Bridge   | ✅ Stable |
| LINE           | Messaging API     | ✅ Stable |
| iMessage       | BlueBubbles       | ✅ Stable |
| Matrix         | Client-Server API | ✅ Stable |
| MS Teams       | Bot Framework     | ✅ Stable |
| Google Chat    | Service Account   | ✅ Stable |
| IRC            | IRCv3             | ✅ Stable |
| Nostr          | NIP-04/44         | ✅ Stable |
| Twitch         | EventSub          | ✅ Stable |
| Feishu (Lark)  | Open API          | ✅ Stable |
| Mattermost     | REST API          | ✅ Stable |
| Nextcloud Talk | REST API          | ✅ Stable |
| Zalo           | OA API            | ✅ Stable |
| Tlon           | HTTP API          | ✅ Stable |
| Synology Chat  | REST API          | ✅ Stable |

## 🤖 Model Providers

### Cloud

Anthropic · OpenAI · Google (Gemini) · Mistral · xAI (Grok) · Z.AI (GLM) · OpenRouter · Perplexity · Together AI · Fireworks · Venice · DeepSeek · Moonshot · Minimax · Kilocode

### Self-Hosted

Ollama · vLLM · SGLang · LiteLLM

### Enterprise

AWS Bedrock · Google Vertex AI · Azure OpenAI · Cloudflare AI Gateway · Vercel AI Gateway · NVIDIA NIM

### HuggingFace

Any HuggingFace model via the HF provider or self-hosted inference endpoints

## 🧠 Autonomous Agent

Deneb includes a built-in autonomous loop engine for continuous, self-directed operation:

- **Goal Management** — Define and track long-running objectives
- **Attention System** — Prioritize tasks and decide when to act
- **Cycle Runner** — Periodic execution with configurable intervals
- **State Persistence** — Survives restarts with full state recovery

```bash
# Start autonomous mode
node deneb.mjs autonomous start

# Check status
node deneb.mjs autonomous status
```

## 🔧 Tools & Capabilities

| Tool       | Description                                          |
| ---------- | ---------------------------------------------------- |
| File I/O   | Read, write, edit files in the workspace             |
| Web Search | Perplexity, Brave Search, or custom providers        |
| Browser    | Playwright-based automation and scraping             |
| PDF        | Native PDF analysis with vision models               |
| Image      | Multi-image analysis with vision models              |
| Memory     | Semantic search across session history and workspace |
| Cron       | Scheduled tasks and heartbeat monitoring             |
| Messaging  | Send messages, reactions, polls across channels      |
| Sub-agents | Spawn bounded sub-agents for complex tasks           |
| MCP        | Model Context Protocol tool integration              |

## 📁 Project Structure

```
Deneb/
├── src/
│   ├── agents/          # Agent loop, sessions, identity, tools
│   ├── autonomous/      # Autonomous loop engine
│   ├── channels/        # Channel plugins and routing
│   ├── cli/             # CLI commands and interactive setup
│   ├── commands/        # Agent commands (browser, dashboard, etc.)
│   ├── config/          # Configuration schema and types
│   ├── gateway/         # Gateway server and daemon management
│   ├── plugins/         # Plugin SDK and bundled providers
│   ├── lcm/             # Lossless Context Management
│   └── auto-reply/      # Message routing and agent invocation
├── extensions/          # Channel extensions
│   ├── telegram/
│   ├── discord/
│   ├── signal/
│   ├── slack/
│   └── ...
├── apps/                # Mobile apps (Android, iOS, macOS)
├── docs/                # Documentation (Mintlify)
└── ui/                  # Web dashboard (React)
```

## 🛠️ Development

```bash
# Install dependencies
pnpm install

# Build
pnpm build

# Run in development mode
pnpm dev

# Run tests
pnpm test

# Run linter
pnpm lint

# Format code
pnpm format

# Type check
pnpm tsgo
```

## 📄 License

MIT

---

<p align="center">
  <img src="https://raw.githubusercontent.com/choiceoh/Deneb/master/ui/public/favicon.svg" alt="⭐" width="40" height="40">
</p>
