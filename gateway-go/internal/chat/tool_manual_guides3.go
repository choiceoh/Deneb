package chat

const memoryGuide = `Deneb memory is plain Markdown in the agent workspace. Files on disk are the source of truth.

## Memory Files
Two-layer layout:
- memory/YYYY-MM-DD.md: daily log (append-only). Today + yesterday loaded at session start.
- MEMORY.md: curated long-term memory (optional). Only loaded in main private session.

Location: agents.defaults.workspace (default ~/.deneb/workspace)

## Memory Tools
- memory_search: keyword/semantic search over MEMORY.md + memory/*.md
- memory_get: read specific file/line range. Degrades gracefully if file missing.

## When to Write
- Decisions, preferences, durable facts -> MEMORY.md
- Day-to-day notes, running context -> memory/YYYY-MM-DD.md

## Auto Memory Flush (Pre-Compaction)
Silent agentic turn before compaction to store durable notes.
Config: agents.defaults.compaction.memoryFlush (enabled by default, softThresholdTokens: 4000)

## Vector Memory Search
- Hybrid: BM25 + vector weighted fusion
- MMR re-ranking for diversity (optional)
- Temporal decay for recency boost (optional, halfLife=30 days)
- Index: ~/.deneb/memory/<agentId>.sqlite
- Providers: local, openai, gemini, voyage, mistral, ollama

## Key Files
- docs/concepts/memory.md
- gateway-go/internal/chat/tool_memory.go`

const sessionsGuide = `Sessions represent individual conversations with lifecycle management.

## Session Keys
- Direct: agent:<agentId>:<mainKey> (default "main")
- Groups: agent:<agentId>:<channel>:group:<id>
- Cron: cron:<job.id>, Webhooks: hook:<uuid>

## DM Scope (session.dmScope)
- main (default): all DMs share one session
- per-peer, per-channel-peer, per-account-channel-peer: isolation options

## Lifecycle State Machine
IDLE -> RUNNING -> DONE / FAILED / KILLED / TIMEOUT
Runs serialized per session key (lane-based queuing).

## Reset Policy
- Daily reset: 4:00 AM local time (default)
- Idle reset: optional sliding window
- Manual: /new or /reset commands

## Session Tools
sessions_list, sessions_history, sessions_search, sessions_restore, sessions_send, sessions_spawn

## Storage
- Store: ~/.deneb/agents/<agentId>/sessions/sessions.json
- Transcripts: <SessionId>.jsonl

## Key Files
- docs/concepts/session.md
- gateway-go/internal/session/`

const architectureGuide = `Deneb: multi-language gateway with three cooperating runtimes.

## Three Runtimes
1. Go Gateway (primary): HTTP/WS, RPC (130+ methods), sessions, auth, cron
2. Rust Core (CGo FFI): protocol, security, media (21 formats), markdown, memory search (SIMD), context engine, compaction
3. Node.js Plugin Host (subprocess): channels, skills, providers via TypeScript SDK

## IPC Boundaries
- Go <-> Rust: CGo FFI (zero overhead, in-process)
- Go <-> Node.js: Unix socket + frame protocol
- CLI <-> Gateway: WebSocket
- Proto schemas: shared cross-language types

## Rust Crates (core-rs/)
- deneb-core: 30+ FFI exports. Modules: protocol, security, media, memory_search, markdown, context_engine, compaction, parsing
- deneb-vega: SQLite FTS5 search
- deneb-ml: GGUF inference (llama-cpp-2, optional cuda)
- deneb-agent-runtime: agent lifecycle, model selection
Feature flags: default -> vega -> ml -> cuda -> dgx

## Hardware Profiles
- DGX Spark: 10 concurrency, 8 embedding batch, CUDA
- Desktop GPU: 8 concurrency, 6 batch, CUDA
- CPU-only: 4 concurrency, 2 batch, software

## Gateway Internal (gateway-go/internal/)
server/, rpc/, session/, channel/, chat/, auth/, ffi/, vega/

## Key Files
- docs/concepts/architecture.md
- gateway-go/cmd/gateway/main.go
- core-rs/core/src/lib.rs`

const channelsGuide = `Channels are messaging surface plugins connecting external platforms to Deneb.

## Channel Plugin Registry
- gateway-go/internal/channel/: Plugin interface with Meta + Capabilities
- Lifecycle manager: concurrent start/stop/health-check

## Primary Channel: Telegram
- 4096-char message limit, MarkdownV2 parse mode
- Inline keyboards, 50 MB file upload limit
- Forum topics: isolated sessions per thread
- Grammy framework

## Channel Routing
1. Channel plugin receives inbound message
2. Session key resolved (dmScope + chat type)
3. Agent run triggered via RPC
4. Response delivered back through same channel

## Supported Channels
Telegram (primary), Discord, Slack, WhatsApp (Baileys), Signal, iMessage, WebChat, extensions

## Design Philosophy
- Telegram-only optimization over cross-channel compatibility
- Single operator, single user
- Korean language first

## Key Files
- gateway-go/internal/channel/registry.go
- docs/channels/telegram.md, channel-routing.md, groups.md`
