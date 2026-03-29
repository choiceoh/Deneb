---
title: Modular Architecture Plan
summary: Go/Rust refactoring roadmap v2 — phased approach to tighten package boundaries, reduce coupling, and improve testability across gateway-go, core-rs, and cli-rs.
read_when:
  - Planning codebase refactoring across Go or Rust modules
  - Understanding package boundaries and FFI contracts
  - Reviewing long-term architecture goals for the gateway
---

# Deneb Modular Architecture Plan

> **v2 — Go/Rust edition.** This document supersedes the earlier TypeScript-era plan. All
> file paths, metrics, and phase targets reflect the current Go/Rust codebase.

## Executive Summary

The Deneb runtime spans three language boundaries — Rust core (`core-rs`), Go gateway
(`gateway-go`), and Rust CLI (`cli-rs`) — connected by CGo FFI and a shared Protobuf schema.
The primary structural issues today are:

1. **`chat` package sprawl** — `gateway-go/internal/chat/` has 65 files (~19K LOC) covering
   system-prompt assembly, tool registration (50+ tools), context files, silent replies, and
   slash commands as a single unstructured package.
2. **`autoreply` density** — `gateway-go/internal/autoreply/` (~18K LOC) mixes dispatch logic,
   model-capability gating, and extended-thinking management.
3. **`rpc` handler dilution** — 130+ RPC methods registered in a single registry
   (`gateway-go/internal/rpc/`) with 14 sub-handler packages that have uneven internal
   boundaries.
4. **Implicit FFI surface** — `gateway-go/internal/ffi/` exposes Rust functions through 7
   `*_cgo.go` files without a stable typed abstraction layer above them.
5. **Proto schema drift risk** — `proto/` generates both Go and Rust types; no automated check
   enforces that consumer packages use only the generated types (not hand-rolled duplicates).

The plan is organized into 4 phases, ordered by risk and dependency.

---

## Current Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                          cli-rs                                   │
│  deneb-rs binary  ─  50+ subcommands, WebSocket client            │
└────────────────────────────┬─────────────────────────────────────┘
                             │  WebSocket (local)
┌────────────────────────────▼─────────────────────────────────────┐
│                     gateway-go  (primary runtime)                 │
│                                                                   │
│  cmd/gateway/main.go                                              │
│       ↓                                                           │
│  internal/server/   ─  HTTP server, /health, /api/v1/rpc, hooks  │
│       ↓                                                           │
│  internal/rpc/      ─  registry, dispatcher, 130+ methods        │
│       ↓                  ↓              ↓             ↓          │
│  internal/chat/  internal/session/  internal/channel/  internal/auth/  │
│  (65 files)      (14 files)         (29 files)         (12 files) │
│       ↓                                                           │
│  internal/ffi/      ─  CGo bindings to Rust core                 │
│  (7 *_cgo.go files)                                               │
└────────────────────────────┬─────────────────────────────────────┘
                             │  CGo (in-process)
┌────────────────────────────▼─────────────────────────────────────┐
│                        core-rs  (static library)                  │
│                                                                   │
│  deneb-core      ─  FFI exports, protocol, security, media,       │
│                     memory_search, markdown, context_engine,       │
│                     compaction, parsing                            │
│  deneb-vega      ─  SQLite FTS5 search engine                     │
│  deneb-agent-runtime  ─  agent lifecycle, model selection         │
└──────────────────────────────────────────────────────────────────┘

Proto schema (proto/) ──── generates ────► Go (pkg/protocol/gen/)
                      └─── generates ────► Rust (core/src/protocol/)
```

### Current Package Metrics

| Package | Files | Approx LOC | Coupling | Risk |
| --- | --- | --- | --- | --- |
| `internal/chat/` | 65 | ~19K | ffi, llm, memory, skills, plugin, provider | HIGH |
| `internal/autoreply/` | 22 | ~18K | thinking, llm, config, process, event, agent | HIGH |
| `internal/rpc/` + `rpc/handler/` | 34 | ~8K | session, chat, channel, node, provider | MEDIUM |
| `internal/server/` | 38 | ~7K | rpc, middleware, auth, channel | MEDIUM |
| `internal/ffi/` | 26 | ~2K | CGo (no Go deps; Rust static lib) | MEDIUM |
| `internal/channel/` | 29 | ~2K | telegram, discord | LOW |
| `internal/session/` | 14 | ~1K | channel, event | LOW |
| `internal/auth/` | 12 | ~900 | config | LOW |
| `internal/provider/` | 24 | ~1K | config, llm | LOW |

---

## Phase 1: Package Boundary Formalization (Low Risk)

**Goal:** Define explicit public interfaces for each `internal/*` package without moving files.
Pass `make check` (fmt + vet + all tests) before and after every change in this phase.

### 1.1 Identify and document package contracts

For each high-coupling package, write a `doc.go` file that declares:

- **Exported types** that are part of the stable API
- **Unexported types** that are implementation details
- **Allowed callers** (packages permitted to import this package)

Priority order:

```
internal/ffi/       — defines CGo boundary; document every exported Go wrapper
internal/session/   — state machine contract (IDLE→RUNNING→DONE/FAILED/KILLED/TIMEOUT)
internal/channel/   — Plugin interface, ChannelRegistry, lifecycle manager
internal/rpc/       — method registration contract, dispatcher interface
internal/auth/      — token validation, allowlist, credential types
```

### 1.2 Enforce one-way dependency direction

Current allowed direction (top → bottom):

```
cmd/gateway/
     ↓
server/
     ↓
rpc/ + rpc/handler/
     ↓
chat/   autoreply/   session/   channel/   provider/   auth/
     ↓
ffi/   llm/   memory/   skills/   config/   logging/
     ↓
(no internal imports)
```

Violations to fix:

- `ffi/` must not import any `internal/*` except `logging/`
- `config/` must not import `chat/`, `autoreply/`, or `rpc/`
- `session/` must not import `chat/` or `rpc/`

Add `go vet` shadow checks and a `go-lint` target that flags circular or upward imports.

### 1.3 Verification gate

All of the following must pass after phase 1:

```bash
make go-vet          # zero warnings
make go-test         # all tests pass
go mod tidy          # no unused deps
```

---

## Phase 2: Chat and Autoreply Decomposition (Medium Risk)

**Goal:** Reduce `chat/` and `autoreply/` from catch-all packages into cohesive sub-packages.
Each sub-package gets its own `_test.go` files. Gate every sub-phase with `make go-test`.

### 2.1 Extract from `internal/chat/`

Current `chat/` contains these distinct concerns:

| Sub-package | Current files | Description |
| --- | --- | --- |
| `chat/systemprompt/` | `system_prompt.go`, runtime assembly | System-prompt builder (identity, tooling, skills, memory, workspace) |
| `chat/tools/` | `tools/fs.go`, `toolreg_core.go`, `tool_schemas_gen.go` | Tool registry, schemas, FS/web/exec implementations |
| `chat/contextfiles/` | `context_files.go` | Workspace context file loader (AGENTS.md, CLAUDE.md, etc.) |
| `chat/dispatch/` | `silent_reply.go`, `slash_commands.go` | Slash command pre-processing, SILENT_REPLY detection |
| `chat/` (core) | remaining files | LLM dispatch, streaming, session plumbing |

**Migration rule:** move files one sub-package at a time; each move is its own commit with
`refactor(chat):` prefix. Do not change logic during file moves.

### 2.2 Extract from `internal/autoreply/`

| Sub-package | Current location | Description |
| --- | --- | --- |
| `autoreply/thinking/` | `autoreply/thinking/` (already exists) | Extended thinking, model-capability gating |
| `autoreply/pipeline/` | top-level autoreply files | Message dispatch pipeline, routing |
| `autoreply/` (core) | remaining files | Entry points, orchestration |

### 2.3 Handler grouping in `internal/rpc/handler/`

Consolidate the 14 sub-handler packages into domain groups:

```
rpc/handler/
  agent/      — agent lifecycle, subagent, tools
  chat/       — chat session, context, streaming
  channel/    — channel health, status, registration
  session/    — session state, history, reset
  node/       — node management
  system/     — config, auth, monitoring
```

Each group registers itself via a `Register(registry *rpc.Registry)` function, matching the
existing registry pattern.

### 2.4 Verification gate

```bash
make check        # proto-check + rust-test + go-test
make go-vet       # no new warnings introduced
```

---

## Phase 3: FFI Abstraction Layer (Medium Risk)

**Goal:** Insert a typed Go abstraction above the raw CGo calls in `internal/ffi/` so that
callers never reference `C.*` types directly, and the no-FFI fallback (`*_noffi.go`) stays
in sync automatically.

### 3.1 Current FFI modules

| CGo file | Rust functions bound | Purpose |
| --- | --- | --- |
| `core_cgo.go` | `deneb_validate_frame`, `deneb_constant_time_eq`, `deneb_detect_mime`, `deneb_validate_session_key`, `deneb_sanitize_html`, `deneb_is_safe_url`, `deneb_validate_error_code`, `deneb_validate_params` | Validation, crypto, MIME, HTML sanitization, URL safety |
| `memory_cgo.go` | `deneb_memory_cosine_similarity`, `deneb_memory_bm25_rank_to_score`, `deneb_memory_build_fts_query`, `deneb_memory_merge_hybrid_results`, `deneb_memory_extract_keywords` | SIMD vector similarity, BM25, FTS, hybrid merge |
| `markdown_cgo.go` | `deneb_markdown_to_ir`, `deneb_markdown_detect_fences` | Markdown parsing, fence detection |
| `parsing_cgo.go` | `deneb_parsing_extract_links`, `deneb_parsing_html_to_markdown`, `deneb_parsing_base64_decode`, `deneb_parsing_media_token_parse` | Link extraction, HTML conversion, media tokens |
| `context_engine_cgo.go` | `deneb_context_assembly_new/start/step`, `deneb_context_expand_new/start/step`, `deneb_context_engine_drop` | Aurora context assembly (handle-based) |
| `compaction_cgo.go` | `deneb_compaction_evaluate`, `deneb_compaction_sweep_new/start/step/drop` | Compaction evaluation, sweep state machine |
| `vega_cgo.go` | `deneb_vega_*` | SQLite FTS5 search |

### 3.2 Add a typed service layer

Introduce `ffi/services/` with Go interfaces that mirror Rust module boundaries:

```go
// ffi/services/security.go
type SecurityService interface {
    ConstantTimeEq(a, b []byte) bool
    SanitizeHTML(input string) (string, error)
    IsSafeURL(url string) (bool, error)
    IsValidSessionKey(key string) bool
}

// ffi/services/memory.go
type MemoryService interface {
    CosineSimilarity(a, b []float32) (float32, error)
    BM25Score(tf, df, n, avgdl float64) float64
    BuildFTSQuery(terms []string) (string, error)
    MergeHybridResults(vector, bm25 []SearchResult, alpha float64) ([]SearchResult, error)
    ExtractKeywords(text string, maxKeywords int) ([]string, error)
}

// ffi/services/context_engine.go
type ContextEngine interface {
    NewAssembly(params AssemblyParams) (Handle, error)
    Start(handle Handle) error
    Step(handle Handle, response []byte) (StepResult, error)
    Drop(handle Handle)
}
```

The CGo implementations in `*_cgo.go` satisfy these interfaces; the no-FFI stubs in
`*_noffi.go` provide safe defaults for pure-Go builds.

### 3.3 Handle-based lifecycle contract

The stateful FFI pattern (`_new` → `_start` → `_step` → `_drop`) must be wrapped
in a Go object that enforces correct lifecycle:

```go
// Enforces: Start before Step, Drop always called (via defer)
type ContextAssemblyHandle struct { /* ... */ }

func (h *ContextAssemblyHandle) Run(ctx context.Context, params AssemblyParams,
    responses <-chan []byte) (*AssemblyResult, error)
```

### 3.4 Verification gate

```bash
make rust           # libdeneb_core.a rebuilt
make go-test        # all Go tests pass with CGo
make go-test-pure   # all Go tests pass without CGo (noffi fallbacks)
```

---

## Phase 4: Proto Schema Integrity (Low Risk, High Value)

**Goal:** Make schema drift between `proto/`, Rust types, and Go types impossible to miss at
commit time.

### 4.1 Current code-generation pipelines

| Source | Generated target | Command |
| --- | --- | --- |
| `proto/gateway.proto` | `core-rs/core/src/protocol/error_codes.rs` | `make proto-error-codes-gen` |
| `proto/gateway.proto` | `gateway-go/pkg/protocol/gen/*.pb.go` | `make proto-go` |
| `core-rs/core/src/ffi_utils.rs` | `gateway-go/internal/ffi/ffi_error_codes_gen.go` | `make ffi-gen` |
| `gateway-go/internal/chat/tool_schemas.yaml` | `internal/chat/tool_schemas_gen.go` | `make tool-schemas` |
| `autoreply/thinking/model_caps.yaml` | `thinking/model_caps_gen.go` | `make model-caps` |

### 4.2 Consistency tests

`gateway-go/pkg/protocol/consistency_test.go` already validates hand-written JSON wire types
against generated protobuf types. Extend this pattern to:

- Verify every `ErrorCode` in `proto/gateway.proto` has a matching entry in `ffi_error_codes_gen.go`
- Verify every tool in `tool_schemas.yaml` has a corresponding registration in `toolreg_core.go`
- Verify every model in `model_caps.yaml` has a Go constant in the `thinking` package

### 4.3 CI enforcement

`make generate-check` (already exists) runs `git diff --exit-code` after regenerating all
outputs. This must be a required check in every PR that touches `proto/`, `core-rs/`, or
generated files.

```bash
make generate-check   # fails if any generated file is out of date
make check            # proto-check + rust-test + go-test (required before push)
```

---

## Implementation Priority and Effort

| Phase | Effort | Risk | Value | Priority |
| --- | --- | --- | --- | --- |
| 1. Package boundary formalization | Small (1 week) | Low | High | **P0** |
| 4. Proto schema integrity | Small (2-3 days) | Low | High | **P0** |
| 2. Chat and autoreply decomposition | Medium (2-3 weeks) | Medium | High | **P1** |
| 3. FFI abstraction layer | Medium (1-2 weeks) | Medium | Medium | **P1** |

---

## Success Criteria

These are measurable gates, each verifiable via `make check` or `go vet`:

- [ ] `make check` passes with zero warnings on a clean checkout
- [ ] `make generate-check` passes — no generated files are stale
- [ ] `make go-test-pure` passes — no logic depends on CGo being present
- [ ] `internal/ffi/` callers use typed service interfaces, not raw `C.*` types
- [ ] `internal/chat/` file count below 25 (remainder extracted to sub-packages)
- [ ] `internal/autoreply/` file count below 10 (remainder extracted to sub-packages)
- [ ] No `internal/config/` or `internal/session/` files import `internal/chat/` or `internal/rpc/`
- [ ] Every `rpc/handler/` sub-package has at least one `*_test.go` file
- [ ] Consistency tests cover all `ErrorCode` entries, all tools, all model capability entries

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| File moves break `go build` (import cycle) | HIGH | Move files without logic changes; run `make go-test` after each move |
| CGo abstraction layer adds allocation overhead | MEDIUM | Profile hot paths; keep service interfaces thin (no extra copies) |
| `*_noffi.go` stubs diverge from CGo behavior | MEDIUM | Shared test cases in `ffi_test.go` run against both build tags |
| Proto regeneration breaks Rust prost types | MEDIUM | `make proto-check` catches drift before merge |
| Multi-agent conflicts during large refactor | MEDIUM | Scope each PR to a single sub-package; use `refactor(<scope>):` commit prefix |

---

## Relationship to `make check`

Every phase is gated on `make check`. The targets it runs:

```
make check
  ├── make proto-check     → regenerate proto + git diff (catches schema drift)
  ├── make rust-test       → cargo test --workspace (all Rust unit tests)
  ├── make go-test         → go test ./... (all Go tests, with CGo)
  └── make ts              → TypeScript checks (docs tooling only)
```

**Hard gate:** do not push any phase without `make check` passing locally.

Fast iteration: `make rust-debug` (debug symbols, faster) + `make go-dev` (auto-restart) is
the recommended inner loop during refactor work. Run `make check` before each commit.
