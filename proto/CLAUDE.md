# Protobuf Schemas

Shared type definitions compiled to Go and Rust. Source of truth for cross-language types.

## Commands

| Command | Description |
|---------|-------------|
| `make proto` | Generate Go + Rust code (parallel) |
| `make proto-go` | Generate Go structs only |
| `make proto-rust` | Generate Rust structs only |
| `make proto-check` | Generate + verify no uncommitted diffs |
| `make proto-lint` | Lint proto files (buf lint) |
| `make proto-watch` | Watch and regenerate on change |

## Schema Files

| File | Contents |
|------|----------|
| `gateway.proto` | `ErrorCode` enum, `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion` |
| `channel.proto` | `ChannelCapabilities`, `ChannelMeta`, `ChannelAccountSnapshot` |
| `session.proto` | `SessionRunStatus`, `SessionKind`, `GatewaySessionRow`, `SessionTransition` |
| `plugin.proto` | Plugin metadata and configuration types |
| `provider.proto` | LLM provider types |
| `agent.proto` | Agent lifecycle types |

## Code Generation Outputs

- **Go:** `gateway-go/pkg/protocol/gen/*.pb.go` (via `buf` + `protoc-gen-go`)
- **Rust:** Automatic via `prost-build` in `core-rs/core/build.rs` (output to `OUT_DIR`)

## After Editing .proto Files

1. Run `make proto` to regenerate Go + Rust code
2. Run `make rust` to rebuild Rust core (proto codegen happens in `build.rs`)
3. Run `make go` to rebuild Go gateway
4. Run `make proto-check` to verify no uncommitted diffs

## Error Code Sync

When changing `ErrorCode` enum in `gateway.proto`, you must update 3 files:

1. `gateway.proto` — proto enum (`ERROR_CODE_*` prefix)
2. `core-rs/core/src/protocol/error_codes.rs` — Rust `as_str()` match block
3. `gateway-go/internal/ffi/errors.go` — Go constants

Run `make error-code-sync` to validate consistency.

## Config

- `buf.yaml` — buf lint and breaking change config
- `buf.gen.go.yaml` — Go codegen config
- Generation script: `scripts/proto-gen.sh`
