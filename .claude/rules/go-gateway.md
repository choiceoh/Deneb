---
description: "Go 게이트웨이 서버 구조/빌드/테스트 규칙"
globs: ["gateway-go/**"]
---

# Go Gateway (`gateway-go/`)

Primary runtime — HTTP/WS gateway server.

## Structure

- `cmd/gateway/main.go` — Entry point with `--port`/`--bind` flags, graceful shutdown.
- `internal/runtime/server/` — HTTP server: `/health`, `/api/v1/miniapp/rpc`, OpenAI/Responses APIs, hooks, session endpoints. Connection tracking.
- `internal/runtime/rpc/` — Registry-based RPC method dispatcher (thread-safe). 150+ methods.
- `internal/runtime/session/` — Session management with lifecycle state machine (`IDLE -> RUNNING -> DONE/FAILED/KILLED/TIMEOUT`), state transition validation, event pub/sub bus.
- `internal/infra/auth/` — Token auth, allowlists, security paths, credentials, probe auth.
- `pkg/protocol/` — Hand-written JSON wire types.
- `internal/pipeline/chat/toolctx/` — Leaf package: shared types (ToolFunc, ToolDef, ToolRegistrar, ToolExecutor), context helpers (WithDeliveryContext, etc.), TurnContext, RunCache, dependency structs (CoreToolDeps, ProcessDeps, SessionDeps, etc.). Zero intra-chat imports.
- `internal/pipeline/chat/toolreg/` — Tool registration hub: wires tool implementations (from tools/) with JSON schemas (from tool_schemas_gen.go) into a ToolRegistrar. Contains tool_schemas.json (codegen source) and tool_schemas_gen.go (generated). Never imports chat/.
- `internal/pipeline/chat/tools/` — Pure tool implementations (fs, exec, git, health, vega, message, kv, gmail, etc.). Depends only on toolctx/ for types.
- `internal/pipeline/chat/toolreg_core.go` — Thin wrapper: calls toolreg.RegisterCoreTools() + registers pilot tool (localai-coupled).
- `internal/pipeline/chat/prompt/system_prompt.go` — System prompt assembly (identity, tooling, tool call style, safety, skills, memory recall, workspace, reply tags, messaging, timestamp, context files, silent replies, runtime).
- `internal/pipeline/chat/prompt/context_files.go` — Workspace context file loader (AGENTS.md, CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md). Budget: 20K chars/file, 150K total.
- `internal/pipeline/chat/silent_reply.go` — SILENT_REPLY_TOKEN (NO_REPLY) detection and stripping for delivery suppression.
- `internal/pipeline/chat/slash_commands.go` — Slash command pre-processing (operational only: /reset, /status, /kill, /rollback, /update, /restart — user commands moved to the native UI).
- `internal/ai/llm/types.go` — Sampling parameters: top_p, top_k, stop_sequences, frequency_penalty, presence_penalty. ImageSource for multimodal content.
- `internal/ai/modelcaps/` — Leaf package: builtin model capability defaults (reasoning model prefixes, cache_control compatibility). Layered with vLLM /models discovery and deneb.json `models.providers.<id>` overrides (contextWindow/reasoning/vision/promptCache + temperature/topP/topK sampling) by `modelrole.Registry.CapabilityForModel` / `ProfileForModel`. The chat pipeline derives context-budget clamping, cache-marker stripping, image-block stripping, and sampling defaults from it (`internal/pipeline/chat/run_capability.go`). Model health circuit breaker: `internal/ai/modelrole/health.go`.
- `internal/ai/modeltuner/` — Background per-model optimization loop (autonomous PeriodicTask, 6h): aggregates 24h agent logs by model (`agentlog.AggregateByModel`), auto-applies a bounded output-token floor for models that keep hitting the ceiling, one-shot-calibrates newly served vLLM models, persists `~/.deneb/model-stats.json`, notifies only when the recommendation set changes.
## GatewayHub Wiring Rules

- `GatewayHub` is a service container — no business logic, only `Broadcast()` and `Validate()`.
- Hub is built only in `buildHub()`. No other file may create or populate `GatewayHub{}`.
- Handler Deps assembly happens only in `method_registry.go` (inline literals, no adapter layer).
- Handler packages (`internal/runtime/rpc/handler/*`) must NOT import `rpcutil.GatewayHub`.
- Adding a new RPC domain: Hub field → handler Deps → `method_registry.go` wiring → `hub.Validate()` update → snapshot test update.
- Do not add adapter/helper files for Deps wiring. Do not add methods to Hub beyond Broadcast/Validate.
- Registration phases: Early (no Chat) → Session (creates Chat) → Late (Chat-dependent) → WorkflowSideEffects (non-RPC). Add new phases only if absolutely necessary.

## Build & Test

- `cd gateway-go && go build ./...` or `make go`.
- `cd gateway-go && go test ./...` or `make go-test`.
- Follow standard `gofmt`/`go vet` conventions. Run `go vet ./...` before commits.
