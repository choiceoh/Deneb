# Rust Core Library

Rust workspace providing protocol validation, security, media, memory search, markdown parsing, context engine, compaction, and FFI exports for the Go gateway.

## Build & Test

| Command | Description |
|---------|-------------|
| `make rust` | Release build, minimal (no vega/ml) — produces `libdeneb_core.a` |
| `make rust-vega` | Release + Vega FTS search |
| `make rust-debug` | Debug build, all crates (fast iteration) |
| `make rust-test` | Run all workspace tests |
| `make rust-clippy` | Lint all crates |
| `make rust-fmt` | Check formatting |

## Workspace Crates

| Crate | Path | Purpose |
|-------|------|---------|
| `deneb-core` | `core/` | Main crate: FFI exports, protocol, security, media, memory search, markdown, context engine, compaction |
| `deneb-vega` | `vega/` | SQLite FTS5 search engine (optional `ml` feature for semantic search) |
| `deneb-agent-runtime` | `agent-runtime/` | Agent lifecycle, model selection |

## Feature Flags

`vega` → `ml` → `cuda` → `vega-ml` → `dgx` (full DGX Spark)

- `make rust` builds `deneb-core --no-default-features` (CGo static lib only)
- `make rust-vega` adds FTS search
- `make rust-dgx` adds ML + CUDA for production DGX Spark deployment

## FFI Exports

`deneb_*` C FFI functions live in `core/src/ffi/` organised by domain.
`core/src/lib.rs` re-exports all symbols into the crate root.

| FFI file | Domain |
|---|---|
| `core/src/ffi/protocol.rs` | Frame validation, param validation, error codes |
| `core/src/ffi/security.rs` | Constant-time eq, session key, HTML sanitize, URL check |
| `core/src/ffi/media.rs` | MIME-type detection |
| `core/src/ffi/vega.rs` | Vega FTS/semantic search (feature-gated) |
| `core/src/ffi/compaction.rs` | Compaction evaluate + sweep state machine |
| `core/src/ffi/memory_search.rs` | Cosine similarity, BM25, FTS query, hybrid merge |
| `core/src/ffi/parsing.rs` | Link extraction, HTML-to-Markdown, base64, media tokens |
| `core/src/ffi/markdown.rs` | Markdown-to-IR, fenced code block detection |
| `core/src/ffi/context_engine.rs` | Context assembly/expansion state machines |

### Adding a New FFI Function

1. Add the function to the appropriate `core/src/ffi/<domain>.rs` file with `#[no_mangle] pub extern "C" fn deneb_*`
2. Create Go wrapper in `gateway-go/internal/ffi/*_cgo.go`
3. Create fallback in `gateway-go/internal/ffi/*_noffi.go`
4. If adding new error codes:
   - Protocol codes: edit `proto/gateway.proto` (`ErrorCode` enum); add `// retryable` on retryable codes.
   - FFI codes: edit `proto/gateway.proto` (`FfiErrorCode` enum); values are positive, negated by generator.
   - Run `make error-codes-gen` to regenerate `error_codes.rs`, `errors_gen.go`, and `ffi_error_codes_gen.go`.
   - Never edit these generated files by hand.
5. Run `make error-codes-gen-check` to verify generated files are up to date.

### Stateful FFI Pattern

For multi-step operations (context engine, compaction):
```
*_new() → handle → *_start(handle) → *_step(handle, response) → *_drop(handle)
```
Handle management: `gateway-go/internal/ffi/handle.go`

## Protobuf Code Generation

Automatic via `prost-build` in `core/build.rs`. Requires `protoc` installed.

Proto files: `proto/gateway.proto`, `channel.proto`, `session.proto`, `plugin.proto`, `provider.proto`, `agent.proto`

Generated output goes to `OUT_DIR`, included by `core/src/protocol/gen.rs`.

## Key Source Directories

| Path | Purpose |
|------|---------|
| `core/src/lib.rs` | Module re-exports; `ffi/` submodule declarations |
| `core/src/ffi/` | C FFI exports organised by domain (30+ `deneb_*` functions) |
| `core/src/protocol/` | Gateway frame validation, error codes |
| `core/src/security/` | `constant_time_eq`, `sanitize_html`, `is_safe_url`, `is_valid_session_key` |
| `core/src/media/` | Magic-byte MIME detection (21 formats), MIME-to-extension (35+ types) |
| `core/src/memory_search/` | SIMD cosine similarity, BM25, FTS query builder, hybrid search |
| `core/src/markdown/` | Markdown-to-IR parser, fenced code block detection |
| `core/src/context_engine/` | Aurora context assembly/expansion state machines |
| `core/src/compaction/` | Compaction evaluation and sweep state machines |
| `core/src/parsing/` | Link extraction, HTML-to-Markdown, base64, media token parsing |

## Linting

Workspace lint config in root `Cargo.toml`:
- `clippy::unwrap_used` = deny (use `map_err`/`ok_or` instead)
- `unsafe_code` = deny (except FFI exports in `core/src/ffi/`)
- `print_stdout`/`print_stderr` = deny
