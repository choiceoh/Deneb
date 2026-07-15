# Chat recall preflight map

Owns cue detection, multi-source evidence collection, ranking/budgeting,
and fenced recall blocks injected into the last user message. Parent chat
calls `Build`; this package must not own turn execution.

## Entry points

- `recall_preflight.go` — `Build` (main preflight orchestration)
- `types.go` — `Params`, `Deps`, `OrgLoader`
- `recall_evidence.go` — source collectors / ranking helpers
- `recall_cache.go` — `CachedSnapshot`, `StoreSnapshot`, `ClearSession`,
  `ShouldFreeze`, `CueFingerprint`
- `recall_provenance.go`, `recall_temporal.go`, `recall_org.go` — specialized
  evidence paths

## Dependency direction and invariants

- **Dependency / port boundary**: consume wiki/knowledge/session through
  `Deps` ports — do not import `runtime/server` or register tools. Keep
  import direction toward domain ports, never upward into chat parent
  orchestration beyond shared `toolport` types.
- **Invariant**: no cue ⇒ empty recall (never invent evidence); panic in a
  source must be recovered and recorded, never abort the turn; fence tags
  inside evidence must be scrubbed before formatting; cache freeze only
  when `ShouldFreeze` says so (cue + non-truncated snapshot).
- Must keep source run order stable for attribution tests even when
  collectors finish concurrently.

## Local change scope

Recall policy and fencing stay in `recall/`.

- May co-change: chat `run_tail_inject` / prepare path that calls `Build`,
  and knowledge/wiki adapters behind `Deps`.
- Do not touch: prompt-cache system prompt assembly, tool registry wiring,
  or genesis skill evolution.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/recall
```
