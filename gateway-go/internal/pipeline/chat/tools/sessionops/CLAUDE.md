# Sessionops (sessions / sessions_spawn)

Owns agent-facing session listing, transcript search, spawn, and sub-agent
steer/kill. Parent `runtimeops` no longer implements this surface.

`sessions` covers both planes: peer sessions (list/history/search/send/stats)
and this session's own children (list with scope="children", plus result/kill/
steer). The separate `subagents` tool folded into it on 2026-08-29 — it was the
same Manager.List() filtered by SpawnedBy, so the split only ever asked the
model a routing question with no principled answer.

## Entry points

- `sessions_tool.go` — `ToolSessions` (dispatches the child actions into
  `ToolSubagents`), `ToolSessionsSpawn`
- `subagents_tool.go` — `ToolSubagents`, now an internal implementation of the
  child plane rather than its own registered tool

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
