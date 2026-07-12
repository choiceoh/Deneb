# Workfeed domain change map

This package owns persisted proactive work items, their engagement state, and
the deterministic actions a user can take on them. RPC and proactive delivery
project these records; they must not reproduce acknowledgement or snooze rules.

## Entry points

- `store.go`: `Store`, `NewStore`, `Item`, and `Action` define persistence and
  the stable domain model. `Store.AppendIfNew` owns consecutive deduplication.
- `Store.ListFiltered` owns time, acknowledgement, snooze, priority, and limit
  ordering. Filtering must happen before the limit is applied.
- `Store.RunAction` is the single action state machine for acknowledge, open,
  follow-up, snooze, and trash outcomes.
- `Store.Engagement` and `EngagementStat` calculate read/action/staleness
  signals. `item_helpers.go` owns preview and inferred-priority helpers.

## Dependency direction and invariants

- Workfeed is a domain package and must never import runtime, RPC, or chat
  implementations. Higher layers translate `Item` and `ActionResult` to wire
  payloads.
- Store mutations must retain atomic file replacement and lock ownership; a
  partial write must never replace the last readable feed.
- Duplicate IDs are settled consistently by every state-changing action.
- Snoozed items remain persisted but invisible until their re-surface time.
  Priority ordering is applied only after visibility filters.
- Empty bodies are never deduplicated, and source identity is part of the
  duplicate key.

## Focused verification

Use `store_test.go` for `Store` mutation/action contracts,
`store_snooze_test.go` for re-surface behavior, and `store_priority_test.go`
for priority and retention.

`cd gateway-go && go test -count=1 ./internal/domain/workfeed`
