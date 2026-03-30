<h1 align="center">Deneb</h1>

<p align="center">
  <strong>Self-Hosted AI Agent with Lossless Memory</strong><br>
  <a href="https://github.com/choiceoh/Deneb/releases"><img src="https://img.shields.io/badge/version-3.28.1-blue" alt="Version"></a>
  <a href="https://github.com/choiceoh/Deneb/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License"></a>
  <a href="https://www.rust-lang.org/"><img src="https://img.shields.io/badge/Rust-stable-dea584" alt="Rust"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8" alt="Go"></a>
</p>

---

**Deneb** is a self-hosted AI agent framework focused on one thing: **never losing context**. A ~155K-line multi-language server engine (Rust + Go) with a custom Aurora context engine — DAG-based compaction, proactive background summarization, and full memory recall across sessions.

**Memory-first, local-first, lean-first.**

## Why Deneb

Most AI agent frameworks hit the same wall: when conversations grow long, context gets compressed and important details vanish. Deneb solves this with **lossless memory** — every decision, number, name, and technical detail is preserved through intelligent compaction and multi-layer recall.

### What's Different

|                        | Typical Agent Framework  | Deneb                                             |
| ---------------------- | ------------------------ | ------------------------------------------------- |
| **Long conversations** | Summarize → lose details | DAG-based compaction preserves everything         |
| **Context recall**     | Vector search only       | Semantic search + DAG expansion + memory files    |
| **Compaction latency** | Blocks on LLM call       | Background observer pre-computes summaries        |
| **Memory persistence** | Session-scoped           | Workspace files + JSONL transcripts + Aurora DAG  |
| **Performance**        | Pure JS/Python           | Rust core (FFI) + Go gateway                     |
| **Local LLM**          | Optional                 | First-class: SGLang, Ollama, vLLM support         |
| **Autonomy**           | Reactive only            | Goal-driven autonomous loop with attention system |

### Intentional Simplification

We deliberately support fewer channels and architectures — **a smaller surface lets us move faster and ship fewer bugs.**

- **One channel done right** (Telegram) > many channels done halfway
- **Every feature battle-tested in production** before landing in the repo
- **Faster iteration** — fewer platforms to test, fewer edge cases to chase

## Key Features

### Aurora Context Engine

The core differentiator. A DAG-structured summary system that compresses conversations without losing details.

- **DAG-based compaction** — Summaries reference each other in a directed acyclic graph, enabling deep recall of any past detail
- **Custom identifier preservation** — Project names, amounts, dates, and technical terms survive compression intact
- **Background observer** — Proactively pre-computes summaries using local LLM, so compaction is instant when triggered
- **Multi-layer recall** — `aurora_grep` → `aurora_describe` → `aurora_expand` → `aurora_expand_query` for progressively deeper memory retrieval
- **Quality guard** — Automatic retry with conservative settings if summary quality drops

### Autonomous Loop Engine

24/7 self-directed operation for proactive monitoring and task execution.

- **Attention system** — Priority queue for signals from channels, goals, and deadlines
- **Goal tracking** — Define, track progress, and manage deadlines
- **State persistence** — Survives restarts with corruption recovery
- **Configurable cycles** — Adjustable interval (default 5min) with rate limiting

### Multi-Agent Orchestration

Spawn and manage sub-agents with bounded contexts for complex tasks.

- **Sub-agent sessions** — Each with independent context and lifecycle
- **Bounded execution** — Token limits, timeouts, and tool policy per agent
- **Result streaming** — Real-time progress from sub-agents to parent
- **ACP (Agent Control Protocol)** — Standardized protocol for agent spawning and control

### Vega Search Engine

Rust-native search engine with SQLite FTS5 and optional ML-powered semantic search.

- **Full-text search** — FTS5-backed keyword search with BM25 ranking
- **Semantic search** — GGUF-based embedding and reranking (optional CUDA acceleration)
- **Hybrid search** — Combines FTS and semantic results with score merging
- **SIMD-accelerated** — Cosine similarity with SIMD optimization

### Media Processing

Rich media understanding and extraction pipeline.

- **YouTube transcripts** — Full transcript extraction with timestamp mapping
- **Video frame extraction** — Key frame capture for vision analysis
- **Telegram attachments** — Photo, video, document, and voice message processing
- **Link understanding** — Automatic content extraction from shared URLs
- **Vision analysis** — Multi-image analysis with vision-capable models

### Skill System

Extensible skill plugins for domain-specific capabilities (16 skills):

`coding-agent` · `github` · `gog` · `healthcheck` · `himalaya` · `imsg` · `mcporter` · `nano-pdf` · `node-connect` · `sag` · `session-logs` · `skill-creator` · `summarize` · `tmux` · `weather` · `xurl`

### Messaging Channels

**Telegram** is the only channel with full, battle-tested production support — reactions, inline buttons, polls, topics, group policies, and all core features.

Other channels are not supported.

### Tool System

| Tool          | Description                                           |
| ------------- | ----------------------------------------------------- |
| File I/O      | Read, write, edit, grep, find, ls workspace files     |
| Exec          | Shell execution and background process management     |
| Web           | Web fetch, search (Perplexity, Brave, auto-detect)    |
| Media         | YouTube transcripts, image/video analysis             |
| Memory        | Semantic search + Aurora expansion + memory retrieval |
| Cron          | Scheduled tasks, reminders, periodic jobs             |
| Sub-agents    | Spawn bounded sub-agents with session isolation       |
| Messaging     | Send, reply, react across channels                    |
| Sessions      | List, history, spawn, steer active sessions           |
| Nodes         | Discover, monitor, and run commands on connected nodes|
| Gateway       | Restart, config, update gateway operations            |
| Apply Patch   | Apply unified diffs to workspace files                |
| MCP           | Model Context Protocol tool integration               |

### Model Providers

**Cloud:** Anthropic · DeepSeek · Google (Gemini) · Mistral · OpenAI · OpenRouter · Perplexity · Together AI · xAI (Grok) · Z.AI (GLM)

**Self-Hosted:** Ollama · SGLang · vLLM · LiteLLM

**Enterprise:** AWS Bedrock · Azure OpenAI · Google Vertex AI

## Quick Start

### Prerequisites

- Rust (stable, via rustup) — for core-rs
- Go 1.24+ — for gateway-go
- An LLM API key (or a local model server)

### Install

```bash
git clone https://github.com/choiceoh/Deneb.git
cd Deneb
make all        # Build Rust + Go
```

### Configure

Edit `~/.deneb/deneb.json` directly.

### Run

```bash
# Start the gateway
scripts/start-go-gateway.sh --port 18789 --bind loopback
```

## Architecture

```
Deneb/
├── core-rs/                    # Rust core library (~45K lines)
│   ├── core/                   #   Protocol, security, media, markdown, memory, context engine
│   ├── vega/                   #   Vega search engine (FTS5 + optional semantic ML)
│   ├── ml/                     #   GGUF inference (llama.cpp, optional CUDA)
│   └── agent-runtime/          #   Agent lifecycle & model selection
├── gateway-go/                 # Go gateway server (~109K lines)
│   ├── cmd/gateway/            #   Entry point
│   └── internal/               #   41 packages: server, RPC, session, channel,
│                               #   chat, agent, autonomous, media, vega, and more
├── proto/                      # Shared Protobuf schemas (6 schemas)
│   ├── gateway.proto           #   Gateway frames, error codes
│   ├── channel.proto           #   Channel capabilities & meta
│   ├── session.proto           #   Session lifecycle & state
│   ├── agent.proto             #   Agent definitions
│   ├── plugin.proto            #   Plugin interface
│   └── provider.proto          #   Provider types
├── cli-rs/                     # Rust CLI (~8K lines)
├── skills/                     # Skill plugins (16 skills)
└── docs/                       # Mintlify documentation site
```

### Multi-Language IPC

- **Go ↔ Rust:** CGo FFI (in-process, zero overhead via static linking)
- **Proto schemas** are the cross-language source of truth

## Development

```bash
make all              # Build Rust + Go
make test             # Run Rust + Go tests
make check            # Full check (proto + Rust + Go)

make rust             # Build Rust only (minimal)
make rust-vega        # Build Rust + Vega search (FTS)
make rust-dgx         # Build Rust + Vega + ML + CUDA (DGX Spark)
make rust-test        # Run Rust tests
make go               # Build Go only
make go-test          # Run Go tests
make proto            # Generate protobuf code
make gateway-dgx      # Full production binary (DGX Spark)
```

## License

MIT

---
