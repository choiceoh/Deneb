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
| `internal/runtime/server/` | HTTP server: `/health`, `/api/v1/miniapp/rpc`, OpenAI APIs, hooks |
| `internal/runtime/rpc/` | Registry-based RPC dispatcher, 150+ methods |
| `internal/runtime/session/` | Session lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`) |
| `internal/pipeline/chat/` | System prompt, tool registration, context files, slash commands |
| `internal/ai/llm/` | LLM client, sampling parameters, multimodal types |
| `internal/platform/` | Channel-side integrations (gmail, gmailpoll, calendar, cron, media) |
| `pkg/protocol/` | Hand-written JSON wire types |

## Common Tasks

### Adding a New RPC Method
Follow the GatewayHub wiring rules (`.claude/rules/hub-wiring.md` — enforced by
code review + snapshot test):
1. Define `Deps` struct + `Methods(deps Deps)` in the handler package (`internal/runtime/rpc/handler/<domain>/`)
2. Add the service field to `rpcutil.GatewayHub` (new domains only) + update `hub.Validate()`
3. Wire the Deps inline in `internal/runtime/server/method_registry.go` (the ONLY wiring point)
4. Update the `requiredMethods` snapshot list in `method_registry_test.go`

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

## Tool Interception & Safety

Tool dispatch is a **single flat registry lookup** — `ToolRegistry.Execute` in
`internal/pipeline/chat/tools.go`. There is no ordered interception chain; all
state is closed over at registration time (see `docs/research/tool-interception-gap.md`).

When you need to intervene in a tool call, use the supported extension points —
**do not add a side-chain or adapter layer**:

- **Pre-execution block / audit** → `StreamHooks.OnBeforeToolCall` in
  `internal/agentsys/agent/hooks.go`. It can only *block* a call (returns
  `block, blockReason`), not handle it. Wire it via `HookCompositor.SetBeforeToolCall`.
- **Post-execution side effects on specific tools** → `PostProcessRegistry`
  (name-matched, runs after execution).
- **Adding a tool** (including from a future plugin/provider) → `RegisterTool`.
  The provider already owns its name; no interception is needed.

Re-registering an existing tool name **silently replaces** the prior definition
and logs a `slog.Warn` so collisions are visible in the operator log. If a plugin
might collide with a core tool name, **namespace it** (e.g. `honcho:search`)
rather than relying on last-writer-wins.

