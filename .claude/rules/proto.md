---
description: "Protobuf 스키마 생성/검증 규칙"
globs: ["proto/**", "gateway-go/pkg/protocol/gen/**"]
---

# Protobuf Schemas (`proto/`)

Shared type definitions compiled to Go and Rust.

## Files

- `gateway.proto` — `ErrorCode` enum, `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`, `GatewayFrame`, `PresenceEntry`, `HelloOk`.
- `channel.proto` — `ChannelCapabilities`, `ChannelMeta`, `ChannelAccountSnapshot`.
- `session.proto` — `SessionRunStatus`, `SessionKind`, `GatewaySessionRow`, `SessionPreviewItem`, `SessionTransition`, `SessionLifecyclePhase`, `SessionLifecycleEvent`.
- `buf.yaml` — buf lint/breaking config.
- `buf.gen.go.yaml` — Go codegen config (protoc-gen-go).

## Generation

- `./scripts/proto-gen.sh` (parallel Go+Rust). See also `make proto`.
- Outputs: `gateway-go/pkg/protocol/gen/*.pb.go`, Rust via `OUT_DIR`.
- CI: `.github/workflows/proto-check.yml` validates generation + breaking changes on PR.

## Commands

| Command | Description |
|---|---|
| `make proto` | Generate protobuf code (Go + Rust, parallel) |
| `make proto-go` | Generate Go protobuf structs only |
| `make proto-rust` | Generate Rust protobuf structs only |
| `make proto-check` | Generate + verify no uncommitted diffs |
| `make proto-lint` | Lint proto files only (buf lint) |
| `make proto-watch` | Watch proto files and regenerate on change |
