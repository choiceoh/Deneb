---
title: Rust/Go Porting Assessment
summary: Evaluation of TypeScript-to-Rust/Go migration progress across core-rs, gateway-go, and proto.
read_when:
  - Evaluating the current state of the Rust/Go porting effort
  - Planning next phases of the multi-language migration
  - Understanding what has been ported vs what remains in TypeScript
---

# Rust/Go Porting Assessment

**Date:** 2026-03-24

## Executive Summary

| Area                           | Size                                         | Progress    | Status                                 |
| ------------------------------ | -------------------------------------------- | ----------- | -------------------------------------- |
| **Rust core (`core-rs/`)**     | 19,502 LOC, 67 files, 3 crates               | **85%**     | Production, actively expanding         |
| **Go gateway (`gateway-go/`)** | 17,724 LOC (13.5K src + 8.7K test), 78 files | **50-55%**  | Core infra complete, biz logic proxied |
| **Protobuf (`proto/`)**        | 6 proto files, 40+ messages/enums            | **100%**    | Fully built, CI-verified               |
| **IPC architecture**           | CGo FFI + Unix socket bridge                 | **95%**     | Production-ready                       |
| **Overall**                    |                                              | **~60-65%** |                                        |

## 1. Rust Core (`core-rs/`)

### Workspace Structure

| Crate        | LOC    | Tests | Status            |
| ------------ | ------ | ----- | ----------------- |
| `deneb-core` | 19,192 | 419   | Production        |
| `deneb-vega` | 233    | 1     | Early scaffolding |
| `deneb-ml`   | 77     | 0     | Stub only         |

### C FFI Exports: 30 Functions

| Category          | Count | Functions                                                                                                                                             |
| ----------------- | ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Protocol/Security | 8     | `validate_frame`, `constant_time_eq`, `detect_mime`, `validate_session_key`, `sanitize_html`, `is_safe_url`, `validate_error_code`, `validate_params` |
| Compaction        | 4     | `evaluate`, `sweep_new`, `sweep_start`, `sweep_step`                                                                                                  |
| Memory Search     | 4     | `cosine_similarity`, `build_fts_query`, `merge_hybrid_results`, `extract_keywords`                                                                    |
| Context Engine    | 5     | `assembly_start/step`, `expand_new/start/step`                                                                                                        |
| Parsing           | 5     | `extract_links`, `html_to_markdown`, `base64_estimate/canonicalize`, `parse_media_tokens`                                                             |
| Vega (stub)       | 2     | `execute`, `search`                                                                                                                                   |
| ML (stub)         | 2     | `embed`, `rerank`                                                                                                                                     |

### Fully Ported Modules (TypeScript replaced by Rust)

| Module                | LOC    | Tests | Replaces                                         |
| --------------------- | ------ | ----- | ------------------------------------------------ |
| `protocol/`           | 1,207  | 19    | AJV frame validation                             |
| `security/`           | 332    | 10    | Constant-time eq, SSRF, HTML sanitize            |
| `media/`              | 477    | --    | MIME detection (21 formats, zero-alloc)          |
| `markdown/`           | 2,849  | 54    | Markdown parsing/rendering with SIMD             |
| `memory_search/`      | 2,800+ | 91    | Cosine (AVX2), BM25, FTS, MMR, temporal decay    |
| `compaction/`         | 2,444  | 45    | LCM hierarchical summarization state machine     |
| `context_engine/`     | 2,335  | 35    | Assembly + retrieval (grep/describe/expand)      |
| `parsing/`            | 1,490  | 35    | URL extraction, HTML-to-MD, base64, media tokens |
| `exif.rs`             | 258    | --    | JPEG EXIF orientation (pure binary)              |
| `png.rs`              | 235    | --    | PNG RGBA encoding                                |
| `safe_regex.rs`       | 421    | 14    | ReDoS prevention                                 |
| `external_content.rs` | 300    | --    | Prompt injection detection (14 patterns)         |

### Not Yet Implemented

- **`deneb-vega`**: Project search engine (Python `vega/` port) -- SQLite FTS5 not yet implemented
- **`deneb-ml`**: Local ML inference (GGUF/llama.cpp) -- stub only, no real implementation

## 2. Go Gateway (`gateway-go/`)

### Size Comparison with TypeScript Gateway

| Metric                                | TypeScript (`src/gateway/`) | Go (`gateway-go/`) | Ratio          |
| ------------------------------------- | --------------------------- | ------------------ | -------------- |
| Source LOC (excl. tests)              | ~49,000                     | ~13,500            | 28%            |
| Total LOC (incl. tests)               | ~93,000                     | ~17,724            | 19%            |
| RPC methods (TS base)                 | 122                         | --                 | --             |
| Go-native RPC methods                 | --                          | 19                 | 16% of TS base |
| Go-wrapped (bridge-forwarded) methods | --                          | ~46                | 38% of TS base |
| Total Go RPC methods                  | --                          | ~65                | 53% of TS base |
| Test files                            | 87                          | 54                 | --             |
| Test-to-code ratio                    | --                          | 49%                | Excellent      |

### Key Distinction: Native vs. Forwarded Methods

The Go gateway has **19 fully Go-native** RPC methods and **~46 bridge-forwarded** methods.

**Go-native methods** (full logic in Go + Rust FFI):

- `health.check`, `system.info`
- `sessions.list`, `sessions.get`, `sessions.delete`
- `channels.list`, `channels.get`, `channels.status`
- `protocol.validate`
- `security.validate_session_key`, `security.sanitize_html`, `security.is_safe_url`, `security.validate_error_code`
- `media.detect_mime`
- `parsing.extract_links`, `parsing.html_to_markdown`, `parsing.base64_estimate`, `parsing.base64_canonicalize`, `parsing.media_tokens`

**Go-wrapped methods** (Go handles auth/sanitization/session state, forwards business logic to Node.js):

- `chat.send/history/abort/inject` -- Go sanitization + abort control, forwarded to bridge
- `agents.*` -- lifecycle tracking in Go, execution forwarded
- `config.*` -- partial Go implementation + bridge forwarding
- `events.*` -- subscription management in Go
- `monitoring.*`, `cron.*`, `hooks.*`, `plugins.*`, `providers.*`, `tools.*`, `vega.*`

### Fully Implemented Go Packages

| Package                | LOC (src) | LOC (test) | Description                                     |
| ---------------------- | --------- | ---------- | ----------------------------------------------- |
| `internal/server/`     | 1,578     | 446        | HTTP/WS server, health, CSP, CORS               |
| `internal/rpc/`        | 2,187     | 1,016      | RPC dispatcher + 13 method groups               |
| `internal/session/`    | 752       | 698        | Session lifecycle state machine                 |
| `internal/channel/`    | 1,396     | 620        | Channel plugin registry + adapter pattern       |
| `internal/bridge/`     | 639       | 486        | Node.js Unix socket IPC + reconnect             |
| `internal/ffi/`        | 560       | 290        | Rust FFI bindings + pure-Go fallbacks           |
| `internal/chat/`       | 552       | 335        | Chat handler (sanitize, abort, history)         |
| `internal/auth/`       | 560       | 568        | Token HMAC, RBAC scopes, device pairing         |
| `internal/events/`     | 900       | 768        | Broadcaster with RBAC + slow consumer detection |
| `internal/config/`     | 1,141     | 737        | Config loading, bootstrap, runtime resolution   |
| `internal/monitoring/` | 730       | 257        | Watchdog, channel health, activity tracking     |
| `internal/process/`    | 339       | 189        | Exec with approval workflow, output capture     |
| `internal/provider/`   | 1,796     | 597        | Provider plugin registry + auth manager         |
| `pkg/protocol/`        | 1,411     | 925        | Frame types + protobuf gen + consistency tests  |

### TS RPC Methods NOT in Go (~57 methods)

| Category                | Count    | Examples                            |
| ----------------------- | -------- | ----------------------------------- |
| `exec.approvals.*`      | 5        | Approval request/resolve workflow   |
| `node.*` (advanced)     | ~10      | Node pairing, canvas, pending queue |
| `device.*`              | 7        | Device pairing/token management     |
| `cron.*` (CRUD)         | ~4       | Add/update/remove/run               |
| `agents.*` (advanced)   | ~4       | File ops, create/delete             |
| `config.*` (advanced)   | ~3       | Apply/patch/schema                  |
| `skills.*`              | ~3       | Install/update/bins                 |
| `wizard/secrets/talk`   | ~6       | Admin interactive features          |
| Dynamic channel methods | variable | Plugin-registered gateway methods   |

## 3. Protobuf Infrastructure (`proto/`) -- 100% Complete

| File             | Package          | Key Types                                                                                  |
| ---------------- | ---------------- | ------------------------------------------------------------------------------------------ |
| `gateway.proto`  | `deneb.gateway`  | ErrorCode (14), RequestFrame, ResponseFrame, EventFrame, ErrorShape, StateVersion, HelloOk |
| `channel.proto`  | `deneb.channel`  | ChannelCapabilities, ChannelMeta, ChannelAccountSnapshot                                   |
| `session.proto`  | `deneb.session`  | SessionRunStatus, SessionKind, GatewaySessionRow, SessionTransition, SessionLifecyclePhase |
| `agent.proto`    | `deneb.agent`    | AgentStatus, AgentSpawnRequest, AgentStatusUpdate, AgentExecutionResult                    |
| `plugin.proto`   | `deneb.plugin`   | PluginKind, PluginMeta, PluginHealthStatus, PluginRegistrySnapshot                         |
| `provider.proto` | `deneb.provider` | ProviderMeta, ProviderAuthMethod, ProviderCatalogEntry                                     |

**Generated outputs:**

- Go: `gateway-go/pkg/protocol/gen/*.pb.go` (3,117 LOC)
- TypeScript: `src/protocol/generated/*.ts` (544 LOC)
- Rust: via prost-build in `OUT_DIR`

**Tooling:** `scripts/proto-gen.sh` (parallel Go+Rust+TS), `make proto-check` (CI), `consistency_test.go` (bidirectional type sync).

## 4. IPC Architecture

```
+----------------+   CGo FFI (static link, 30 functions)   +----------------+
|   Go Gateway   | <--------------------------------------> |   Rust Core    |
|   13.5K LOC    |   libdeneb_core.a                        |   19.5K LOC    |
+-------+--------+                                         +----------------+
        |
        | Unix Socket + NDJSON (25 MB max, 32 KB buffer)
        | Reconnect: exponential backoff 1s -> 30s
        |
+-------v--------+
|    Node.js     |  <- Unported business logic delegated here
|  Plugin Host   |  <- TypeScript plugin-sdk extensions
|   ~49K LOC     |
+----------------+
```

**Design strengths:**

- `*_noffi.go` files enable `CGO_ENABLED=0` pure-Go builds (Docker/CI)
- macOS builds link `-framework Security` for Keychain
- All FFI functions wrapped in `ffi_catch` panic guard
- Separate `writeMu`/`mu` locks in bridge prevent read blocking during writes
- `safeGo()` wrapper with panic recovery prevents goroutine crashes

## 5. Strengths

1. **CPU-intensive paths fully in Rust** -- frame validation, MIME detection, HTML sanitization, secret comparison, cosine similarity (AVX2), markdown parsing (SIMD)
2. **419 Rust tests + 54 Go test files (49% test-to-code ratio)** -- high confidence in ported code
3. **Command/response state machine pattern** in compaction/context engine safely crosses FFI boundaries without callbacks
4. **Protobuf as cross-language source of truth** with CI-enforced consistency
5. **Smart proxy architecture** -- Go handles auth/session/routing, forwards complex business logic to Node.js

## 6. Key Gaps

1. **Only 19 of 122 RPC methods are fully Go-native** (16%) -- the remaining ~46 Go methods forward to Node.js
2. **`deneb-ml` unimplemented** -- local embedding/reranking is stub-only
3. **`deneb-vega` early stage** -- project search engine SQLite FTS5 not functional
4. **Business logic layer** -- approval workflows, device management, cron CRUD, skill installation all depend on Node.js bridge
5. **~57 TS RPC methods have no Go equivalent at all** -- not even bridge-forwarded

## 7. Roadmap Suggestion

| Phase        | Target                                                            | Est. LOC | Priority  |
| ------------ | ----------------------------------------------------------------- | -------- | --------- |
| **Done**     | Core server/RPC/FFI/bridge/protocol/session/channel/auth/events   | --       | --        |
| **Phase 2a** | Remaining ~57 RPC methods (approval, device, cron, agent, skills) | ~5-8K    | High      |
| **Phase 2b** | Move bridge-forwarded method logic to native Go                   | ~3-5K    | High      |
| **Phase 3**  | `deneb-vega` SQLite FTS5 full implementation                      | ~1-2K    | Medium    |
| **Phase 4**  | `deneb-ml` GGUF inference implementation                          | TBD      | Low       |
| **Phase 5**  | Minimize Node.js bridge dependency (pure Go+Rust gateway)         | ~10K+    | Long-term |

## 8. Conclusion

The infrastructure foundation is solid: Rust handles all performance-critical and security-sensitive operations, Go provides the server/RPC/session/auth/event framework, and Protobuf ensures cross-language type safety. The Go gateway currently operates as a **smart proxy** -- handling routing, authentication, session state, and monitoring natively while delegating business logic to the Node.js Plugin Host.

The primary remaining work is:

1. Porting the ~57 missing RPC methods to Go (even as bridge-forwarded wrappers)
2. Gradually moving bridge-forwarded business logic into native Go implementations
3. Completing the `deneb-vega` and `deneb-ml` crates
