---
description: "Rust 코어 라이브러리 빌드/테스트/구조 규칙"
globs: ["core-rs/**", "cli-rs/**"]
---

# Rust Core (`core-rs/`)

Rust workspace with 4 crates, exposed to Go via C FFI (CGo static linking).

## Crates

**deneb-core** (main crate, `core/`):
- `src/lib.rs` — 30+ C FFI exports (`deneb_*` functions). FFI error codes generated from `ffi_utils.rs`; protocol `ErrorCode` generated from `proto/gateway.proto`.
- `src/protocol/` — Gateway frame validation. Types: `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`.
- `src/security/` — `constant_time_eq`, `sanitize_html`, `is_safe_url` (SSRF), `is_valid_session_key`.
- `src/media/` — Magic-byte MIME detection (21 formats), MIME-to-extension mapping (35+ types), `MediaCategory`.
- `src/memory_search/` — SIMD-accelerated cosine similarity, BM25, FTS query builder, hybrid search merge, keyword extraction.
- `src/markdown/` — Markdown-to-IR parser (pulldown-cmark), fenced code block detection.
- `src/context_engine/` — Aurora context assembly/expansion state machines (handle-based FFI).
- `src/compaction/` — Compaction evaluation and sweep state machines.
- `src/parsing/` — Link extraction, HTML-to-Markdown, base64 utilities, media token parsing.
- `build.rs` — prost-build code generation from `proto/*.proto`.
- Crate types: `staticlib` (Go CGo linking), `rlib` (workspace consumers).

**deneb-vega** (`vega/`): SQLite FTS5 search engine. Optional `ml` feature for semantic search.

**deneb-ml** (`ml/`): GGUF inference via llama-cpp-2. Optional `cuda` feature for GPU acceleration.

**deneb-agent-runtime** (`agent-runtime/`): Agent lifecycle, model selection.

## Feature Flags

`vega` -> `ml` -> `cuda` -> `vega-ml` -> `dgx` (full DGX Spark).

## Build & Test

- `make rust` (minimal), `make rust-vega` (FTS), `make rust-dgx` (full).
- `cd core-rs && cargo test` or `make rust-test`.
- Follow `cargo fmt`/`cargo clippy` conventions. Run `cargo clippy --workspace` before commits.
