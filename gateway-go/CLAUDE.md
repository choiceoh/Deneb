# Go Gateway Module

Go HTTP/WS gateway server — the primary Deneb runtime.

## Build & Test

| Command | Description |
|---------|-------------|
| `make go` | Build (CGo, requires `make rust` first) |
| `make go-pure` | Build without Rust (`CGO_ENABLED=0 -tags no_ffi`) |
| `make go-dev` | Dev mode with auto-restart on SIGUSR1 |
| `make go-test` | Run tests with `-race` |
| `make go-test-pure` | Run tests without FFI |
| `make go-vet` | Run `go vet` |
| `make go-fmt` | Check formatting |

## Directory Map

| Directory | Purpose |
|-----------|---------|
| `cmd/gateway/` | Entry point (`main.go`), `--port`/`--bind` flags, graceful shutdown |
| `internal/server/` | HTTP server: `/health`, `/api/v1/rpc`, OpenAI APIs, hooks |
| `internal/rpc/` | Registry-based RPC dispatcher, 130+ methods |
| `internal/session/` | Session lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`) |
| `internal/channel/` | Channel plugin registry (`Plugin` interface, lifecycle manager) |
| `internal/chat/` | System prompt, tool registration, context files, slash commands |
| `internal/ffi/` | CGo bindings to Rust core (8 modules) |
| `internal/auth/` | Token auth, allowlists, credentials |
| `internal/llm/` | LLM client, sampling parameters, multimodal types |
| `internal/vega/` | Vega search integration, model auto-detection |
| `internal/telegram/` | Telegram channel plugin (primary deployment target) |
| `pkg/protocol/` | Hand-written JSON wire types + generated protobuf types in `gen/` |

## FFI Pattern

Rust core is linked as a static library via CGo. Each FFI module has two files:

- `*_cgo.go` — CGo implementation (build tag: `!no_ffi && cgo`)
- `*_noffi.go` — Pure-Go fallback (build tag: `no_ffi || !cgo`)

Modules: `core`, `memory`, `markdown`, `parsing`, `context_engine`, `compaction`, `vega`

FFI error codes in `internal/ffi/ffi_error_codes_gen.go` are generated from `core-rs/core/src/ffi_utils.rs` via `make ffi-gen`.
Protocol error codes (`ErrorCode` enum) are defined in `proto/gateway.proto` and auto-generated into `core-rs/core/src/protocol/error_codes.rs` via `make proto-error-codes-gen`.

## Common Tasks

### Adding a New RPC Method
1. Define the method in `internal/rpc/methods.go`
2. Register in `internal/rpc/register.go`
3. Add handler in `internal/rpc/handler/`
4. Follow existing patterns for request/response types

### Adding a New Agent Tool
1. Register tool schema in `internal/chat/tools_core.go`
2. Implement handler in `internal/chat/tools_fs.go` (or new file for non-FS tools)
3. Tool schemas use full JSON Schema definitions

### Modifying System Prompt
- Assembly: `internal/chat/system_prompt.go`
- Context files: `internal/chat/context_files.go` (loads CLAUDE.md, SOUL.md, etc.)
- Silent replies: `internal/chat/silent_reply.go` (NO_REPLY token)
- Slash commands: `internal/chat/slash_commands.go` (/reset, /status, /kill, /model, /think)

### Changing Wire Types
- Hand-written types: `pkg/protocol/`
- Generated types: `pkg/protocol/gen/` (from `make proto`)
- **Must pass**: `pkg/protocol/consistency_test.go` (bidirectional sync check)
