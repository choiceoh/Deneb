---
description: "Go 게이트웨이 서버 구조/빌드/테스트 규칙"
globs: ["gateway-go/**"]
---

# Go Gateway (`gateway-go/`)

Primary runtime — HTTP/WS gateway server.

## Structure

- `cmd/gateway/main.go` — Entry point with `--port`/`--bind` flags, graceful shutdown.
- `internal/server/` — HTTP server: `/health`, `/api/v1/rpc`, OpenAI/Responses APIs, hooks, session endpoints. Connection tracking.
- `internal/rpc/` — Registry-based RPC method dispatcher (thread-safe). 130+ methods including FFI-backed security/media/memory/context/compaction.
- `internal/session/` — Session management with lifecycle state machine (`IDLE -> RUNNING -> DONE/FAILED/KILLED/TIMEOUT`), state transition validation, event pub/sub bus.
- `internal/channel/` — Channel plugin registry with `Plugin` interface, `Meta`, `Capabilities`. Lifecycle manager for concurrent start/stop/health-check orchestration.
- `internal/ffi/` — CGo bindings to Rust core (8 `*_cgo.go` files + `*_noffi.go` fallbacks). Build tag: `!no_ffi && cgo`.
- `internal/auth/` — Token auth, allowlists, security paths, credentials, probe auth.
- `pkg/protocol/` — Hand-written JSON wire types + generated protobuf types in `gen/`.
- `pkg/protocol/consistency_test.go` — Bidirectional reflection tests ensuring hand-written and generated types stay in sync.
- `internal/chat/toolreg_core.go` — Core tool registration (exec, process, read, write, edit, grep, find, ls, web) with full JSON schemas.
- `internal/chat/tools/fs.go` — File system tool implementations (read with line numbers, write with dir creation, edit with uniqueness check, grep via rg, find via WalkDir, ls).
- `internal/chat/system_prompt.go` — System prompt assembly (identity, tooling, tool call style, safety, skills, memory recall, workspace, reply tags, messaging, timestamp, context files, silent replies, runtime).
- `internal/chat/context_files.go` — Workspace context file loader (AGENTS.md, CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md). Budget: 20K chars/file, 150K total.
- `internal/chat/silent_reply.go` — SILENT_REPLY_TOKEN (NO_REPLY) detection and stripping for delivery suppression.
- `internal/chat/slash_commands.go` — Slash command pre-processing (/reset, /status, /kill, /model, /think).
- `internal/llm/types.go` — Sampling parameters: top_p, top_k, stop_sequences, frequency_penalty, presence_penalty. ImageSource for multimodal content.

## Build & Test

- `cd gateway-go && go build ./...` or `make go`.
- `cd gateway-go && go test ./...` or `make go-test`.
- Follow standard `gofmt`/`go vet` conventions. Run `go vet ./...` before commits.
