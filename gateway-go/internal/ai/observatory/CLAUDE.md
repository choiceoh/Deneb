# Observatory self-status digest map

Aggregates scattered self-improvement telemetry into one compact
machine-readable `Report` for agents (not a human dashboard).

## Entry points

- `observatory.go` — `Snapshot` → `Report` (frontier, failures, loops,
  skills, memory, models)
- `digest.go` — `Report.Markdown` rendering helpers
- Types: `Report`, `FrontierItem`, `FailureCount`, `LoopStatus`,
  `SkillSummary`, `MemoryStatus`, `ModelSummary`

## Dependency direction and invariants

- **Dependency / boundary**: read-only over state directories under the
  operator home — must not import chat turn execution, RPC handlers, or
  mutate genesis queues. Prefer filesystem snapshots over live service
  handles.
- **Invariant**: `Snapshot` must always return a complete `Report` shape
  even when some sources are missing (empty slices / zero counts, never
  nil panics); ages and paths must stay deterministic for a given `now`.
- Never invent skill or model rows that are not present on disk.

## Local change scope

Keep digest aggregation local to this package.

- May co-change: callers that surface the digest (CLI/RPC) and the state
  file layouts it reads.
- Do not touch: genesis evolution engines or chat recall.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/ai/observatory
```
