---
title: TypeScript Migration Language Recommendations
summary: Rust vs Go recommendations for each TypeScript component pending migration.
read_when:
  - Planning which language to use for migrating TypeScript modules
  - Deciding between Rust and Go for a specific gateway subsystem
  - Reviewing the multi-language migration roadmap
---

# TypeScript Migration Language Recommendations

Recommendations for each remaining TypeScript component, based on the established patterns in `core-rs/` (Rust) and `gateway-go/` (Go).

## Guiding Principles

| Criterion | Rust | Go |
| --- | --- | --- |
| CPU-intensive computation | ✅ | |
| Security-critical / crypto | ✅ | |
| Deterministic latency (no GC) | ✅ | |
| I/O orchestration / fan-out | | ✅ |
| HTTP/WebSocket serving | | ✅ |
| Subprocess / process management | | ✅ |
| Concurrent state coordination | | ✅ |
| Plugin host / bridge management | | ✅ |

---

## Component Recommendations

| Component | Recommended | Rationale |
| --- | --- | --- |
| **Chat Handler** (send parsing, auth, event dispatch) | **Go** | I/O routing and dispatch; mirrors existing `internal/rpc/` pattern. Thin Go handler forwards to Plugin Host when needed. |
| **Inbound Dispatch** (channel receive, abort/stop, message routing) | **Go** | Event fan-out and channel coordination; fits Go's goroutine concurrency model. Extends `internal/channel/` lifecycle manager. |
| **Pre-LLM Processing** (model selection, vision, reply format, URL understanding) | **Go + Rust FFI** | Go orchestrates the pipeline. Heavy parsing (URL extraction, media analysis) calls Rust FFI. Model selection is config-driven logic suited to Go. |
| **Agent Execution Loop** (prompt assembly, orchestration, retry, failover) | **Go** | Orchestration-heavy with retries, timeouts, and context cancellation. Go's `context.Context` and goroutines are ideal. |
| **LLM API Call** (request assembly, streaming, provider serialization) | **Go** | HTTP client with SSE/streaming. Go's `net/http` + `io.Reader` streaming is well-suited. Provider serialization is straightforward struct marshaling. |
| **Context Engine** (ingest, assemble, compact, afterTurn) | **Rust** | Compaction already in Rust (`core-rs/core/src/compaction/`). Remaining ingest/assemble/afterTurn phases should follow. CPU-bound token counting and chunk selection benefit from zero-allocation Rust. |
| **Provider Connectors** (OpenAI, Anthropic, OAuth, API key, model discovery) | **Go** | HTTP clients, OAuth flows, key rotation, discovery polling. I/O-bound with retries. Extends `internal/provider/` registry. |
| **Tool Execution** (bash, exec, web search, image gen, file ops) | **Go** | Process spawning with approval workflows already in `internal/process/`. Subprocess management, pipe I/O, and timeout enforcement are Go strengths. |
| **Response Formatting & Lifecycle** (ReplyPayload serialization, channel routing) | **Go** | Routing to correct channel adapter is orchestration. Payload serialization is simple struct marshaling. Extends existing `internal/rpc/` response path. |
| **Session Transcript & Storage** (JSONL history, file write) | **Go** | File I/O with buffered writes. Extends `internal/session/` manager. JSONL append is straightforward in Go. |
| **Built-in Channels** (Discord, Telegram, Slack adapters) | **TypeScript (via bridge)** | Channel SDKs (`discord.js`, `grammy`, `@slack/bolt`) are Node.js-only. Keep in Plugin Host, accessed through existing Unix socket bridge. No migration needed. |
| **Plugin Discovery** (manifest, loader, SDK contract gate) | **Go** | Registry pattern already established in `internal/channel/registry.go`. Manifest loading and validation fit Go's file/JSON handling. |
| **Memory / Search** (conversation memory storage and retrieval) | **Rust + Go** | **Rust** for indexing, embedding similarity, and search ranking (CPU-intensive). **Go** for storage I/O and query coordination. Follows the Vega pattern (`core-rs/vega/`). |
| **RPC Methods** (~113 methods, ~99 remaining) | **Go** | Direct extension of `internal/rpc/methods.go`. Business logic handlers with session/channel/config access. Go's interface-based dispatch is already proven for 12+ methods. |

---

## Summary

```
┌─────────────────────────────────────────────────────┐
│                      Rust (core-rs)                  │
│                                                      │
│  Context Engine (ingest/assemble/compact/afterTurn)  │
│  Memory Search (indexing, ranking, similarity)       │
│  Pre-LLM heavy parsing (URL, media analysis via FFI)│
│  + existing: protocol, security, MIME, markdown,     │
│              compaction, safe-regex                   │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│                    Go (gateway-go)                    │
│                                                      │
│  Chat Handler          Agent Execution Loop          │
│  Inbound Dispatch      LLM API Call + Streaming      │
│  Provider Connectors   Tool Execution                │
│  Response Formatting   Session Transcript            │
│  Plugin Discovery      RPC Methods (~99 remaining)   │
│  Pre-LLM orchestration Memory Storage I/O            │
│  + existing: server, sessions, channels, bridge,     │
│              auth, events, cron, monitoring           │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│              TypeScript (Plugin Host bridge)          │
│                                                      │
│  Built-in Channels (Discord, Telegram, Slack, etc.)  │
│  Extension SDK + channel plugins                     │
│  + Node.js-only SDK dependencies                     │
└─────────────────────────────────────────────────────┘
```

### Migration Priority

1. **RPC Methods** — highest leverage; ~99 methods to port, each is independent. Unblocks Plugin Host slimming.
2. **Context Engine** — compaction already in Rust; complete the remaining phases for full Rust ownership.
3. **Chat Handler + Inbound Dispatch** — core message flow; moving to Go eliminates the largest Node.js hot path.
4. **Agent Execution Loop + LLM API Call** — the execution pipeline; Go's streaming and context cancellation simplify error handling.
5. **Provider Connectors** — independent HTTP clients; parallelizable migration.
6. **Tool Execution** — already scaffolded in Go (`internal/process/`).
7. **Memory/Search** — Rust indexing + Go storage; follows Vega pattern.
8. **Session Transcript** — simple file I/O; low risk.
9. **Plugin Discovery** — registry already patterned in Go.
10. **Response Formatting** — depends on chat handler migration.
