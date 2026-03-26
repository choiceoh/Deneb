# Agent Runtime — Rust Port Plan

> Porting `src/agents/` (180K LOC TypeScript) to Rust as `core-rs/agent-runtime/`.

## Current State

### TypeScript (`src/agents/`)
- ~887 files, ~180K LOC (including tests)
- Largest subsystem after `src/gateway/`
- 368 files across the codebase import from it
- No direct Rust/FFI today — security offloaded to `core-rs/`

### Existing Rust (`core-rs/core/`)
- Protocol validation, security, media, markdown, memory search, compaction, context engine
- FFI: C ABI (Go CGo) + napi-rs (Node.js)
- State machine pattern for compaction/context (yields I/O commands, host executes)

### Existing Go (`gateway-go/internal/`)
- Agent store/CRUD (`internal/agent/store.go`)
- Job tracking with TTL cache (`internal/agent/job.go`)
- Chat loop with tool-use (`internal/chat/agent.go`)
- RPC methods for agent CRUD + session lifecycle

## Architecture Decision

**Hybrid approach:** Port compute-heavy, state-machine-like subsystems to Rust.
Keep I/O-heavy orchestration (HTTP, WebSocket, LLM API calls, plugin loading) in
TypeScript/Go. Expose Rust via C FFI (Go) + napi-rs (Node.js), following
existing `core-rs/` patterns.

### What to port to Rust

| Subsystem | LOC | Rationale |
|-----------|-----|-----------|
| Model selection & catalog | ~3.5K | Pure logic, hot path, no I/O |
| Agent scope/registry | ~340 | Config resolution, pure logic |
| Subagent registry | ~1.5K | State machine, lifecycle tracking |
| Context pruning | ~2K | CPU-intensive, already adjacent to compaction |
| System prompt composition | ~2K | String assembly, template logic |
| Schema cleaning | ~500 | JSON transform, Gemini/xAI quirks |
| Tool result sanitization | ~600 | String processing, truncation |
| Session transcript repair | ~580 | JSON processing |
| Sandbox path safety | ~280 | Security-critical path validation |
| Usage normalization | ~300 | Pure arithmetic |

### What stays in TypeScript/Go

| Subsystem | LOC | Rationale |
|-----------|-----|-----------|
| PI embedded runner | ~14K | Heavy I/O (LLM streaming, HTTP) |
| Bash tools/exec | ~2K | Process spawning, OS interaction |
| Auth profiles | ~2K | Credential I/O, OAuth flows |
| Skills/workspace | ~3K | File I/O, plugin loading |
| Sandbox (Docker/SSH) | ~8K | Subprocess orchestration |
| ACP spawn | ~800 | WebSocket/IPC messaging |
| OpenAI WS streaming | ~1K | WebSocket protocol |
| CLI runner | ~1K | Process management |
| Tool implementations | ~5K | HTTP, file I/O, external APIs |

## Phase Plan

### Phase 1: Agent Core Types & Model Selection (Week 1-2)

**New crate:** `core-rs/agent-runtime/` (workspace member)

```
core-rs/agent-runtime/
  Cargo.toml
  src/
    lib.rs
    model/
      mod.rs          # ModelRef, ModelCatalogEntry types
      selection.rs    # parseModelRef, normalizeModelRef, modelKey
      catalog.rs      # findModelInCatalog, loadModelCatalog (from JSON)
      fallback.rs     # provider fallback chain resolution
      defaults.rs     # DEFAULT_MODEL, DEFAULT_PROVIDER, context tokens
    scope/
      mod.rs          # Agent registry types
      resolve.rs      # resolveAgentConfig, resolveDefaultAgentId
      workspace.rs    # workspace path resolution
    usage/
      mod.rs          # UsageSnapshot, NormalizedUsage
      normalize.rs    # hasNonzeroUsage, derivePromptTokens
```

**FFI surface:**
```rust
// C FFI
deneb_agent_parse_model_ref(json_ptr, json_len, out_ptr, out_len) -> i32
deneb_agent_resolve_model_fallback(config_json, agent_id, out_ptr, out_len) -> i32
deneb_agent_resolve_scope(config_json, agent_id, out_ptr, out_len) -> i32
deneb_agent_normalize_usage(usage_json, out_ptr, out_len) -> i32

// napi-rs
#[napi] fn parse_model_ref(input: String) -> ModelRef
#[napi] fn resolve_model_fallback(config: JsObject, agent_id: String) -> ModelFallbackResult
```

**Validation:** TypeScript callers produce identical results. Add cross-language
roundtrip tests (Rust unit tests + TypeScript integration tests comparing outputs).

### Phase 2: Subagent Registry & Lifecycle (Week 3-4)

```
core-rs/agent-runtime/src/
    subagent/
      mod.rs          # SubagentRun, SubagentStatus types
      registry.rs     # register, release, list, count, orphan detection
      depth.rs        # nesting depth limits
      lifecycle.rs    # state transitions (spawning -> running -> completed/failed/killed)
```

**Key design:** State machine yielding events (like compaction sweep pattern).
Host (TS/Go) handles I/O (message delivery, session creation); Rust manages
state transitions, depth checks, orphan recovery logic.

**FFI surface:**
```rust
deneb_subagent_registry_new() -> *mut SubagentRegistry
deneb_subagent_register(registry, run_json) -> i32
deneb_subagent_release(registry, run_id) -> i32
deneb_subagent_list(registry, filter_json, out_ptr, out_len) -> i32
deneb_subagent_detect_orphans(registry, out_ptr, out_len) -> i32
deneb_subagent_registry_drop(registry)
```

### Phase 3: System Prompt & Context Pruning (Week 5-6)

```
core-rs/agent-runtime/src/
    prompt/
      mod.rs          # PromptComposition types
      compose.rs      # system prompt assembly from config + skills + context
      scenarios.rs    # prompt composition scenarios
    pruning/
      mod.rs          # context pruning types
      strategy.rs     # pruning strategies (token budget, priority)
      safeguard.rs    # compaction safeguard logic
```

**Integration:** Extends existing `core-rs/core/compaction/` and
`core-rs/core/context_engine/` modules. Reuse token counting, summary generation
interfaces.

### Phase 4: Security & Sanitization (Week 7-8)

```
core-rs/agent-runtime/src/
    safety/
      mod.rs
      path_safety.rs  # sandbox path validation (directory traversal prevention)
      schema_clean.rs # Gemini/xAI schema quirk cleaning
      transcript.rs   # session transcript repair/sanitization
      tool_result.rs  # tool result truncation & sanitization
```

**Rationale:** Security-critical code benefits most from Rust's memory safety.
Path validation especially — prevents directory traversal in sandbox environments.

### Phase 5: Integration & Migration (Week 9-10)

1. Add `agent-runtime` as dependency of `deneb-core`
2. Re-export FFI functions through `deneb-core` C ABI
3. Add Go CGo bindings in `gateway-go/internal/ffi/`
4. Add napi-rs bindings in `deneb-core` (feature-gated)
5. Create TypeScript wrapper in `src/agents/native/` that calls napi
6. Gradually replace TypeScript implementations with native calls
7. Add `no_ffi` Go fallbacks for all new FFI functions

## Crate Structure

```toml
# core-rs/agent-runtime/Cargo.toml
[package]
name = "deneb-agent-runtime"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["rlib"]  # consumed by deneb-core only

[dependencies]
serde = { workspace = true }
serde_json = { workspace = true }
thiserror = { workspace = true }
regex = { workspace = true }
once_cell = { workspace = true }
```

**Not** a cdylib/staticlib itself — consumed by `deneb-core` which handles FFI.

## Migration Strategy

### Incremental replacement (per function)

1. Implement Rust function with identical semantics
2. Add comprehensive Rust unit tests
3. Add cross-language roundtrip test (TS calls Rust via napi, compares output)
4. Replace TypeScript call site with native call
5. Keep TypeScript implementation as fallback (feature-flagged)
6. Remove TypeScript implementation after stabilization

### Compatibility contract

- All Rust functions must produce byte-identical JSON output to TypeScript
- Proto types (`deneb.agent.*`) remain the shared type source
- FFI follows existing patterns: `deneb_agent_*` prefix, same error codes
- Go `no_ffi` stubs provide pure-Go fallback

## Risk Assessment

| Risk | Mitigation |
|------|-----------|
| Semantic drift during port | Cross-language roundtrip tests |
| FFI overhead for small calls | Batch operations, minimize crossings |
| Build complexity | Single `make agent-runtime` target |
| 368 importers to migrate | Incremental, TypeScript wrapper first |
| Auth/streaming stays in TS | Clear boundary: Rust = pure logic, TS = I/O |

## Success Metrics

- Model selection: <1ms (currently ~5ms in TS)
- Agent scope resolution: <0.5ms
- System prompt composition: <2ms for complex prompts
- Zero regressions in existing test suite
- `make check` passes at every phase boundary

## Non-Goals

- Porting LLM API clients (HTTP/WebSocket) to Rust
- Porting bash tool execution to Rust
- Porting plugin/skill loading to Rust
- Replacing the PI embedded runner event loop
- Multi-user support
