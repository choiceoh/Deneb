# Observeops (runtime self-observation tool)

Owns `ToolObserve` — the agent-facing adapter over `core/observe` log capture,
`core/agentlog` roll-ups, workfeed proactive engagement, and observatory health
snapshots. Parent `runtimeops` no longer imports these leaf deps.

## Entry points

- `observe.go` — `ToolObserve` (actions: turn, logs, behavior, effort,
  proactive, provenance, speed, health)
- `engine_speed_format.go` — renders `enginespeed.Store` days. Keep the "not
  measured" row distinct from a rate, and keep the three caveats attached: the
  prefill figure has the engine's queue removed, the concurrency figure excludes
  idle time, and the peak is only a floor at the poll cadence.

## Dependency direction and invariants

- **Dependency / boundary**: may import observatory, agentlog, observe,
  workfeed, config, toolport, artifact. Must not import parent `runtimeops`
  (facade lives in `toolbind/observebind` instead).
- **Invariant**: nil deps degrade to clear "not wired" messages rather than
  panicking. Health action reads `observatory.Snapshot` only — do not invent a
  second health digest here.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/runtimeops/observeops
```
