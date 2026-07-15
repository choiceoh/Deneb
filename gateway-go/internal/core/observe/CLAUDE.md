# Observe / log capture map

Owns in-process log ring capture, turn views, and vLLM prefix-cache probes
for operator diagnostics. Leaf relative to RPC handlers.

## Entry points

- `capture.go` — `NewRing`, `NewCapture`, `LogCapture`, `ParseLevel`, `QueryOpts`
- `turn.go` — `BuildTurnView`, `TurnView`
- `vllm_cache.go` — `FetchVllmPrefixCaches`, `VllmPrefixCache`

## Dependency direction and invariants

- **Dependency / boundary**: may use `core/agentlog` for turn joins — must
  not import chat, wiki, or RPC handlers. Handlers inject capture into slog.
- **Invariant**: ring capacity is fixed at construction; concurrent writers
  must not block broadcast paths; `BuildTurnView` must tolerate missing
  agentlog entries without panicking.
- Never persist secrets from log lines into durable stores here.

## Local change scope

Keep capture/query changes inside `observe/`.

- May co-change: agentlog writer shape and admin RPC that queries the ring.
- Do not touch: chat run lifecycle or tool execution.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/core/observe
```
