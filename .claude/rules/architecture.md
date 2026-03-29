---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
globs: ["cmd/**", "internal/**", "pkg/**", "src/**"]
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `core-rs/` — Rust core library (protocol validation, security, media, memory search, markdown, context engine, compaction, Vega search, ML inference). Workspace with 4 crates. Builds as staticlib (Go CGo) + rlib.
- `gateway-go/` — Go gateway server (HTTP/WS server, RPC dispatch, session management, channel registry, chat/LLM, tools, auth). The primary runtime.
- `cli-rs/` — Rust CLI entry point.
- `proto/` — shared Protobuf schemas (gateway frames, channel types, session models). Source of truth for cross-language types.
- `skills/` — user-facing skill plugins (github, weather, summarize, coding-agent, etc.).
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.agents/skills/` — maintainer agent skills (release, GHSA, PR, Parallels smoke).
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — multi-language build orchestration (Rust + Go + protobuf).
- Tests: Rust tests inline `#[cfg(test)]`; Go tests `*_test.go`.

## IPC Architecture

- **Go <> Rust:** CGo FFI (in-process, zero overhead). Go calls `deneb_*` C functions from `core-rs/target/release/libdeneb_core.a`.
- **CLI <> Gateway:** WebSocket.
- Proto schemas are the cross-language source of truth for frame types.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/server` (HTTP/WS) -> `internal/rpc` (dispatch) -> `internal/session` (state) -> `internal/channel` (plugins).
2. **Rust FFI flow:** `core-rs/core/src/lib.rs` (C ABI) -> `gateway-go/internal/ffi/*_cgo.go` (Go wrappers) -> RPC methods / chat pipeline.
3. **Protobuf type flow:** `proto/*.proto` -> `scripts/proto-gen.sh` -> Go (`gen/*.pb.go`), Rust (prost `OUT_DIR`).
4. **Stateful FFI pattern:** `*_new()` -> handle -> `*_start(handle)` -> `*_step(handle, response)` -> `*_drop(handle)` (context engine, compaction).

## Cross-Cutting Concerns

- When adding channels/docs, update `.github/labeler.yml` and create matching GitHub labels.
