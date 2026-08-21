# Sessionops (sessions / sessions_spawn / subagents)

Owns agent-facing session listing, transcript search, spawn, and sub-agent
steer/kill. Parent `runtimeops` no longer implements this surface.

## Entry points

- `sessions_tool.go` — `ToolSessions`, `ToolSessionsSpawn`
- `subagents_tool.go` — `ToolSubagents`

Wiring stays in `toolwire/ops.RegisterSessionTools`.

## Dependency direction and invariants

- May import `tooldeps`, `toolport`, `domain/session`, `toolpreset`,
  `artifact`, `jsonutil`, `textutil`. Must not import parent `runtimeops`.
- Spawn validates depth, concurrency, role availability, and the tool preset.
  Terminal children do not count against the cap.
- Search expansion is recall-only: it must not invent matches the transcript
  store did not return.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/sessionops
```
