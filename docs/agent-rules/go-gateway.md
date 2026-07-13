---
description: "Go 게이트웨이 서버 구조/빌드/테스트 규칙"
globs: ["gateway-go/**"]
---

# Go Gateway (`gateway-go/`)

Primary runtime — HTTP + SSE gateway server.

## Structure

- `cmd/gateway/main.go` — Entry point with `--port`/`--bind` flags, graceful shutdown.
- `internal/runtime/server/` — HTTP server: `/health`, `/api/v1/miniapp/rpc`, OpenAI/Responses APIs, hooks, session endpoints. Connection tracking.
- `internal/runtime/rpc/` — Registry-based RPC method dispatcher (thread-safe). 150+ methods.
- `internal/domain/session/` — Session management with lifecycle state machine (`IDLE -> RUNNING -> DONE/FAILED/KILLED/TIMEOUT`), state transition validation, event pub/sub bus.
- `internal/infra/clientauth/` — Native-client token auth (`X-Deneb-Client-Token` verify, identity). Credentials/secret resolution lives in `internal/infra/secret/`, config load/bootstrap in `internal/infra/config/`. (The former single `internal/infra/auth/` package was split across these three.)
- `pkg/protocol/` — Hand-written JSON wire types.
- `internal/pipeline/chat/toolport/` — Leaf package: shared types (ToolFunc, ToolDef, ToolRegistrar, ToolExecutor), context helpers (WithDeliveryContext, etc.), TurnContext, RunCache, display helpers. Zero intra-chat imports and no domain/platform imports.
- `internal/pipeline/chat/tooldeps/` — Dependency bags and capability ports (CoreToolDeps, ProcessDeps, SessionDeps, CalendarDeps, etc.) consumed by tool registration and implementations. Keeps volatile domain/platform wiring out of the stable toolport contract.
- `internal/pipeline/chat/toolreg/` — Tool registration hub: wires tool implementations (from tools/) with JSON schemas (from tool_schemas_gen.go) into a ToolRegistrar. Contains tool_schemas.json (codegen source) and tool_schemas_gen.go (generated). Never imports chat/.
- `internal/pipeline/chat/tools/` — Pure tool implementations (fs, exec, git, health, wiki, message, kv, gmail, etc.). Depends on toolport/ for execution types and tooldeps/ for wiring bags.
- `internal/pipeline/chat/toolreg_core.go` — Thin wrapper: calls toolreg.RegisterCoreTools() + registers pilot tool (localai-coupled).
- `internal/pipeline/chat/prompt/system_prompt.go` — System prompt assembly (identity, tooling, tool call style, safety, skills, memory recall, workspace, reply tags, messaging, timestamp, context files, silent replies, runtime).
- `internal/pipeline/chat/prompt/context_files.go` — Workspace context file loader (AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md). Budget: 8K bytes/file (MEMORY.md 32K), 72K total; oversized files are head+tail truncated with a visible omission marker + Warn log.
- `internal/pipeline/chat/silent_reply.go` — SILENT_REPLY_TOKEN (NO_REPLY) detection and stripping for delivery suppression.
- `internal/pipeline/chat/slash_commands.go` — Slash command pre-processing (/help, /reset, /status, /kill, /goal, /rollback, /update, /restart, /weekly — mostly operational; rich user commands moved to the native UI).
- `internal/ai/llm/types.go` — Sampling parameters: top_p, top_k, stop_sequences, frequency_penalty, presence_penalty. ImageSource for multimodal content.
- `internal/ai/modelcaps/` — Leaf package: builtin model capability defaults (reasoning model prefixes, cache_control compatibility). Layered with vLLM /models discovery and deneb.json `models.providers.<id>` overrides (contextWindow/reasoning/vision/promptCache + temperature/topP/topK sampling) by `modelrole.Registry.CapabilityForModel` / `ProfileForModel`. The chat pipeline derives context-budget clamping, cache-marker stripping, image-block stripping, and sampling defaults from it (`internal/pipeline/chat/run_capability.go`). Model health circuit breaker: `internal/ai/modelrole/health.go`.
- `internal/domain/backup/` — Daily offsite memory backup (autonomous PeriodicTask `memory-backup`): tar.gz of wiki/diary/transcripts/polaris/workspace/contacts/kv streamed over ssh to the storage node (NFS is read-only from the gateway host). Production state dir only; env: `DENEB_BACKUP_SSH_HOST`/`DENEB_BACKUP_DIR`/`DENEB_BACKUP_RETENTION_DAYS`/`DENEB_BACKUP_DISABLE`. The wiki dir is additionally a local git repo (`internal/domain/wiki/gitsnap.go`) committed per dream cycle and before each backup.
- `internal/ai/modeltuner/` — Background per-model optimization loop (autonomous PeriodicTask, 6h): aggregates 24h agent logs by model (`agentlog.AggregateByModel`), auto-applies a bounded output-token floor for models that keep hitting the ceiling, one-shot-calibrates newly served vLLM models, persists `~/.deneb/model-stats.json`. Recommendations surface under the native model picker (`miniapp_models` AdvisoryLines/NoteFor, read from the scorecard) — not as a proactive notification.

> 위 목록은 chat 파이프라인 중심의 상세 노트다. `internal/` 전체(runtime/pipeline/ai/domain/platform/infra/agentsys)의 한 줄 오리엔테이션 맵은 `docs/agent-rules/architecture.md` "Gateway Module Map" 참조.

## GatewayHub Wiring Rules

**정본은 `docs/agent-rules/hub-wiring.md` (5규칙 + 등록 5단계 + 스냅샷 테스트)** —
RPC 핸들러/허브/`method_registry.go`를 만지기 전에 그 파일을 읽는다. 한 줄 요약:
배선은 `method_registry.go` 인라인만, 핸들러는 Deps만 받고 Hub import 금지, 어댑터 파일 금지.

## Build & Test

- `cd gateway-go && go build ./...` or `make go`.
- `cd gateway-go && go test ./...` or `make go-test`.
- Follow `gofumpt` (stricter gofmt superset) / `go vet` conventions — `make fmt` applies gofumpt, golangci-lint enforces it. Run `go vet ./...` before commits.
