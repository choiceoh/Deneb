# Go Gateway Module

Go HTTP/WS gateway server — the primary Deneb runtime.

## Build & Test

| Command | Description |
|---------|-------------|
| `make go` | Build |
| `make go-dev` | Dev mode with auto-restart on SIGUSR1 |
| `make go-test` | Run tests with `-race` |
| `make go-vet` | Run `go vet` |
| `make go-fmt` | Check formatting |

## Directory Map

| Directory | Purpose |
|-----------|---------|
| `cmd/gateway/` | Entry point (`main.go`), `--port`/`--bind` flags, graceful shutdown |
| `internal/runtime/server/` | HTTP server: `/health`, `/api/v1/rpc`, OpenAI APIs, hooks |
| `internal/runtime/rpc/` | Registry-based RPC dispatcher, 150+ methods |
| `internal/runtime/session/` | Session lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`) |
| `internal/pipeline/chat/` | System prompt, tool registration, context files, slash commands |
| `internal/ai/llm/` | LLM client, sampling parameters, multimodal types |
| `internal/platform/` | Channel-side integrations (gmail, gmailpoll, calendar, cron, media) |
| `pkg/protocol/` | Hand-written JSON wire types |

## Common Tasks

### Adding a New RPC Method
1. Define the method in `internal/runtime/rpc/methods.go`
2. Register in `internal/runtime/rpc/register.go`
3. Add handler in `internal/runtime/rpc/handler/`
4. Follow existing patterns for request/response types

### Adding a New Agent Tool
1. Add schema to `internal/pipeline/chat/toolreg/tool_schemas.json`, run `make tool-schemas`
2. Implement handler in `internal/pipeline/chat/tools/<name>.go`
3. Register in `internal/pipeline/chat/toolreg/core.go` (appropriate Register*Tools function)

### Working with Generated Files

Several files in this module are machine-generated. **Never edit them by hand.**

| File | Source | Command |
|------|--------|---------|
| `internal/pipeline/chat/toolreg/tool_schemas_gen.go` | `internal/pipeline/chat/toolreg/tool_schemas.json` | `make tool-schemas` |
| `internal/pipeline/chat/tool_classification_gen.go` | `internal/pipeline/chat/tool_classification.json` | `make data-gen` |

To modify a generated file: edit the source or generator, run the `make` target, commit both together. CI will reject any PR where the generated output diverges from its source.

### Modifying System Prompt
- Assembly: `internal/pipeline/chat/prompt/system_prompt.go`
- Context files: `internal/pipeline/chat/prompt/context_files.go` (loads CLAUDE.md, SOUL.md, etc.)
- Silent replies: `internal/pipeline/chat/silent_reply.go` (NO_REPLY token)
- Slash commands: `internal/pipeline/chat/slash_commands.go` (/reset, /status, /kill, /model, /think)

### Changing Wire Types
- Hand-written types: `pkg/protocol/`

