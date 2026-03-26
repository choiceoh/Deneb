---
title: Go/Rust Migration Assessment
summary: Go gateway and Rust core library migration progress assessment (2026-03-26).
read_when:
  - Checking Go/Rust migration status
  - Understanding multi-language architecture maturity
  - Planning remaining migration work
---

# Go/Rust Migration Assessment

**Assessment date:** 2026-03-26

## Summary

Go gateway and Rust core library have both reached **production-level maturity**. All P0-P3 migration TODO items from CLAUDE.md are **fully implemented**. Node.js bridge has been completely removed. Remaining gaps are minimal.

---

## Go Gateway (`gateway-go/`)

### Scale

| Metric | Value |
|--------|-------|
| Go source files | 498 |
| Test files | 174 (`*_test.go`) |
| Test functions | 1,214 (`func Test*`) |
| Total LOC | 92,748 |
| Internal packages | 36 |
| RPC methods | 130+ (registered) |

### Internal Package Maturity

| Package | src | test | Status |
|---------|-----|------|--------|
| **rpc** | 43 | 8 | Complete -- 130+ methods, parity test |
| **autoreply** | 90 | 45 | Complete -- largest package |
| **chat** | 20 | 9 | Complete -- system prompt, 10 tools, slash commands |
| **cron** | 23 | 14 | Complete -- full scheduling |
| **ffi** | 19 | 8 | Complete -- Rust FFI bindings |
| **plugin** | 16 | 5 | Complete -- plugin registry/lifecycle |
| **provider** | 13 | 11 | Complete -- LLM provider catalog |
| **skills** | 11 | 5 | Complete -- skill discovery/execution |
| **telegram** | 10 | 7 | Complete -- Telegram channel |
| **server** | 9 | 6 | Complete -- HTTP/WS server |
| **auth** | 6 | 6 | Complete -- auth/security |
| **channel** | 6 | 5 | Complete -- channel registry |
| **session** | 6 | 5 | Complete -- session state machine |
| **llm** | 5 | 3 | Complete -- OpenAI/Anthropic clients |
| **config** | 5 | 3 | Complete -- config load/reload |
| **events** | 4 | 4 | Complete -- event bus |
| **vega** | 3 | 1 | Complete -- FFI-based search |

### HTTP Endpoints (All Implemented)

| Endpoint | Implementation |
|----------|---------------|
| `GET /health`, `/healthz` | `server.go` |
| `GET /ready`, `/readyz` | `server.go` |
| `POST /api/v1/rpc` | `server.go` -- main RPC dispatch |
| `GET /ws` | `ws.go` -- WebSocket with challenge auth |
| `POST /tools/invoke` | `http_tools_invoke.go` |
| `POST /sessions/{key}/kill` | `http_session_kill.go` |
| `GET /sessions/{key}/history` | `http_session_history.go` (JSON + SSE) |
| `POST /v1/chat/completions` | `openai_http.go` (482 LOC) |
| `POST /v1/responses` | `responses_http.go` (446 LOC) |
| `POST /hooks/*` | `hooks_http.go` (34KB) + test (24KB) |

### Migration TODO Completion

| Priority | Item | Status |
|----------|------|--------|
| **P0** | Hooks HTTP endpoint | `hooks_http.go` |
| **P1** | OpenAI Chat Completions API | `openai_http.go` |
| **P1** | Open Responses API | `responses_http.go` |
| **P1** | Plugin HTTP routing | `plugin_http.go` |
| **P2** | Tools Invoke HTTP | `http_tools_invoke.go` |
| **P2** | Session Kill HTTP | `http_session_kill.go` |
| **P2** | Session History HTTP | `http_session_history.go` |
| **P2** | `channels.logout` | `server.go` |
| **P2** | Config hot reload | `server.go` |
| **P2** | Auth allowlist + security path | `allowlist.go`, `security_path.go` |
| **P3** | Vega/ML feature flag | `autodetect.go` |
| **P3** | `connect.challenge` event | `ws.go` |
| **P3** | Credential planner / Probe auth | `credentials.go` |

All items complete.

---

## Rust Core Library (`core-rs/`)

### Scale

| Metric | Value |
|--------|-------|
| Rust source files | 109 |
| Total LOC | 36,820 |
| Test functions | 684 (`#[test]`) |
| Workspace crates | 4 (core, vega, ml, agent-runtime) |

### Crate Status

#### `deneb-core` (v3.8.0) -- Production

| Module | Capability | Tests |
|--------|-----------|-------|
| `lib.rs` (FFI) | 8 C function exports | Panic-safe via `ffi_catch` |
| `protocol/` | Frame validation, 14 error codes, 12 param schemas | 18 |
| `security/` | constant_time_eq, SSRF, HTML sanitize, session key validation | 46 |
| `media/` | 21 format magic-byte detection, 48 MIME mappings, OOXML/ISOBMFF | 29 |
| `markdown/` | Parsing, rendering, fence detection | ~80 |
| `memory_search/` | BM25, cosine similarity, MMR, temporal decay | ~50 |
| `context_engine/` | Context assembly/retrieval/caching | ~30 |
| `parsing/` | Media tokens, HTML-to-MD, base64, URL extraction | ~40 |
| `safe_regex.rs` | ReDoS pattern detection | ~30 |

#### `deneb-vega` (v0.1.0) -- Feature Complete

Python-to-Rust port complete. 19 commands implemented (memory, brief, changelog, contacts, cross, dashboard, etc.). SQLite FTS5 + optional semantic search. ~5,500 LOC + AI routing 29K.

#### `deneb-ml` (v0.1.0) -- Early (Feature-Gated)

- Embedding: Qwen3-Embedding-8B
- Reranker: Qwen3-Reranker-4B
- llama-cpp-2 based, CUDA acceleration support
- Without `ml` feature: stub mode (BackendUnavailable)

#### `deneb-agent-runtime` (v0.1.0) -- Complete

Model selection/parsing/catalog/allowlist. 10+ provider ID normalization. Subagent lifecycle, usage normalization.

---

## Protobuf Schemas (`proto/`)

| Aspect | Status |
|--------|--------|
| Proto files | 6 (gateway, channel, session, plugin, provider, agent) |
| Messages | 21 |
| Enums | 6 |
| Go generation | 3 `.pb.go` + 3 hand-written |
| TypeScript generation | 6 `.ts` + barrel |
| Rust generation | prost-build (6 modules) |
| Consistency tests | `consistency_test.go` (6 bidirectional checks) |
| CI integration | `proto-check` (generate + diff verify) |

---

## Overall Score

| Area | Score | Basis |
|------|-------|-------|
| **Go gateway** | **95%** | All HTTP/RPC/WS features. 1,214 tests. Node.js bridge removed. |
| **Rust core** | **95%** | 8 FFI functions complete, 684 tests. Vega ported. |
| **Rust ML** | **40%** | Structure only. CUDA/llama-cpp behind feature flag. |
| **Proto schemas** | **100%** | 3-language generation, consistency tests, CI verification. |
| **Migration TODO** | **100%** | All P0-P3 items complete. |

## Remaining Gaps

1. **ML crate** -- Actual GGUF model inference is feature-gated. Additional work needed when activating on DGX Spark with CUDA.
2. **Build environment** -- `libdeneb_core.a` must be built via `cargo build --release` before Go can link (expected in production build pipeline).
