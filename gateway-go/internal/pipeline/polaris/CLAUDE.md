# Polaris session-store change map

Polaris owns durable message history, summary DAGs, cross-session search, and
background compaction state. Chat uses `Bridge`; compaction supplies algorithms
through narrow ports and does not own Polaris persistence.

## Entry points

- `store.go`: `Store`, `NewStore`, `AppendMessage`, `LoadMessages`, and
  `InsertSummary` own durable messages, indexes, and summary nodes.
- `engine.go`: `Engine`, `NewEngine`, `CompactAndPersist`, and
  `CompactInBackground` own compaction admission, single-flight execution, and
  coverage commits.
- `bridge.go`: `Bridge` adapts Polaris to the `chatport.TranscriptStore`
  contract and performs lazy legacy migration.
- `assemble.go` builds model context from summaries plus uncovered recent
  messages. `semantic.go` and store search methods own recall projections.
- `circuit.go` isolates per-session compaction failures; `sweep.go` removes
  expired files without evicting loaded sessions.

## Dependency direction and invariants

- Polaris may depend on `chatport` DTOs and compaction ports, never the concrete
  chat handler or tool registry.
- A summary's covered message range is committed only after the summary is
  durable. Concurrent appends must remain outside the pinned range.
- Assembly must not drop uncovered messages or leave unmatched tool-use/result
  pairs after trimming.
- Strict bridge mode returns persistence and migration failures. Best-effort
  mode may defer migration, but must retry rather than mark it complete.
- Store operations preserve monotonically increasing message indexes and DAG
  parent links across reopen.

## Focused verification

Use `store_test.go`, `assemble_test.go`, `engine_bg_compact_test.go`, and
`bridge_strict_test.go` for their matching entry points.

`cd gateway-go && go test -count=1 ./internal/pipeline/polaris`
