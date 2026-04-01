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
- `internal/ffi/` — CGo bindings to Rust core (8 `*_cgo.go` files + `*_noffi.go` fallbacks). Build tag: `!no_ffi && cgo`.
- `internal/auth/` — Token auth, allowlists, security paths, credentials, probe auth.
- `pkg/protocol/` — Hand-written JSON wire types + generated protobuf types in `gen/`.
- `pkg/protocol/consistency_test.go` — Bidirectional reflection tests ensuring hand-written and generated types stay in sync.
- `internal/chat/toolctx/` — Leaf package: shared types (ToolFunc, ToolDef, ToolRegistrar, ToolExecutor), context helpers (WithDeliveryContext, etc.), TurnContext, RunCache, dependency structs (CoreToolDeps, ProcessDeps, SessionDeps, etc.). Zero intra-chat imports.
- `internal/chat/toolreg/` — Tool registration hub: wires tool implementations (from tools/) with JSON schemas (from tool_schemas_gen.go) into a ToolRegistrar. Contains tool_schemas.yaml (codegen source) and tool_schemas_gen.go (generated). Never imports chat/.
- `internal/chat/tools/` — Pure tool implementations (fs, exec, git, health, vega, message, kv, gmail, etc.). Depends only on toolctx/ for types.
- `internal/chat/toolreg_core.go` — Thin wrapper: calls toolreg.RegisterCoreTools() + registers pilot tool (sglang-coupled).
- `internal/chat/system_prompt.go` — System prompt assembly (identity, tooling, tool call style, safety, skills, memory recall, workspace, reply tags, messaging, timestamp, context files, silent replies, runtime).
- `internal/chat/context_files.go` — Workspace context file loader (AGENTS.md, CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md). Budget: 20K chars/file, 150K total.
- `internal/chat/silent_reply.go` — SILENT_REPLY_TOKEN (NO_REPLY) detection and stripping for delivery suppression.
- `internal/chat/slash_commands.go` — Slash command pre-processing (/reset, /status, /kill, /model, /think).
- `internal/llm/types.go` — Sampling parameters: top_p, top_k, stop_sequences, frequency_penalty, presence_penalty. ImageSource for multimodal content.
- `internal/server/webhook_github.go` — Inbound GitHub webhook handler (`POST /webhook/github`). HMAC-SHA256 signature verification, Korean Telegram notifications, fires `github.webhook` hook event.

## GitHub Webhook (KAIROS)

Inbound GitHub → Deneb webhook for real-time repo event monitoring.

**Endpoint:** `POST /webhook/github`

**Auth:** HMAC-SHA256 via `X-Hub-Signature-256` (no bearer token — uses GitHub's own signing).

**Supported events:** `ping`, `push`, `pull_request`, `issues`, `issue_comment`, `pull_request_review`, `create`, `delete`, `workflow_run`.

**Config (env vars):**
```
GITHUB_WEBHOOK_SECRET   — shared secret set in GitHub repo/org webhook settings (required; omitting disables the endpoint)
GITHUB_WEBHOOK_CHAT_ID  — Telegram chat ID to deliver Korean-language notifications to
```

**Hook integration:** Each received event fires `hooks.EventGitHubWebhook` (`"github.webhook"`) with env vars `GITHUB_EVENT`, `GITHUB_DELIVERY`, `GITHUB_REPO`, `GITHUB_ACTOR` so user-defined shell hooks can react.

**Delivery:** Notifications are sent directly to Telegram (no LLM round-trip). The handler returns 200 immediately and processes async so GitHub never times out.

**Key files:**
- `internal/server/webhook_github.go` — handler, signature verify, formatters
- `internal/hooks/hooks.go` — `EventGitHubWebhook` constant
- `internal/server/server_http_routing.go` — route registration
- `internal/server/server.go` — `githubWebhookCfg` field, `GitHubWebhookConfigFromEnv()`

## GatewayHub Wiring Rules

- `GatewayHub` is a service container — no business logic, only `Broadcast()` and `Validate()`.
- Hub is built only in `buildHub()`. No other file may create or populate `GatewayHub{}`.
- Handler Deps assembly happens only in `method_registry.go` (inline literals, no adapter layer).
- Handler packages (`internal/rpc/handler/*`) must NOT import `rpcutil.GatewayHub`.
- Adding a new RPC domain: Hub field → handler Deps → `method_registry.go` wiring → `validateHub()` update → snapshot test update.
- Do not add adapter/helper files for Deps wiring. Do not add methods to Hub beyond Broadcast/Validate.
- Registration phases: Early (no Chat) → Session (creates Chat) → Late (Chat-dependent) → WorkflowSideEffects (non-RPC). Add new phases only if absolutely necessary.

## Build & Test

- `cd gateway-go && go build ./...` or `make go`.
- `cd gateway-go && go test ./...` or `make go-test`.
- Follow standard `gofmt`/`go vet` conventions. Run `go vet ./...` before commits.
