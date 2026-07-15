# Agent execution-loop change map

This package owns the provider-stream → assistant turn → tool execution loop
shared by chat and autoreply. Callers provide typed model/tool ports and hooks;
channel delivery and session policy remain outside this package.

## Entry points

- `executor.go`: `RunAgent` is the public orchestration entry. Run-scoped state
  and staged messages are initialized here.
- `executor_runner.go` separates request budgeting, streamed-turn validation,
  usage accounting, recovery, finish gates, tool commits, and grace turns.
- `executor_stream.go` and `executor_stream_accumulator.go` own stream retry,
  block assembly, usage, stop reason, and provider-model consistency.
- `executor_tool_turn.go` owns sequential/parallel tool dispatch and balanced
  synthetic results on cancellation.
- `config.go`: `AgentConfig` is the run contract; `ComposeBeforeAPICall` chains
  message decorators. `client.go` and `tool.go` define narrow ports.
- `file_cache.go`, `spillover.go`, and `line_ranker.go` bound large tool output
  and preserve provenance.

## Dependency direction and invariants

- The agent loop depends on `llm` contracts and a `ToolExecutor`, never on the
  chat handler, runtime server, or channel adapters.
- Every persisted `tool_use` must have one corresponding result, including
  cancellation and malformed-argument paths.
- Provider model and strict stop-shape checks run before tool hooks or
  execution. Denied calls still consume the configured attempt budget.
- A staged assistant message is persisted at most once; recovery may append it
  to history without duplicating the durable transcript.
- Stream retries reset partial accumulation, retain hook semantics, and never
  erase a valid stop reason or usage trailer.


## Local change scope

Keep loop changes inside the agent execution boundary.

- Safe neighbors: `internal/ai/llm` (stream contracts), caller-provided
  `ToolExecutor` ports, and this package's `*_test.go` files
  (`executor_integration_test.go` first).
- Do not touch: `internal/pipeline/chat` handler wiring,
  `internal/runtime/server` composition, channel delivery adapters.
- Focused verify: `cd gateway-go && go test -count=1 ./internal/ai/agent`

## Focused verification

Start with `executor_integration_test.go`, then the matching recovery,
cancellation, stream, or message-journal test file.

`cd gateway-go && go test -count=1 ./internal/ai/agent`

Concurrency changes also require `go test -race -count=1 ./internal/ai/agent`.
