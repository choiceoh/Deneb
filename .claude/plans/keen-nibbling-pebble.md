# Fix aurora-dream all-zeros reporting & add early exit

## Context

The aurora-dream cycle completes with `verified=0 merged=0 expired=0 pruned=0 patterns=0` after ~8 minutes. Investigation reveals **3 bugs**, not a single root cause:

1. **Phases 4/5 are invisible**: `updateUserModel` and `synthesizeMutualUnderstanding` make LLM calls and update the KV store, but their outcomes aren't tracked in the report. When they're the only phases doing work, the report shows all zeros — looks broken.
2. **FactsPruned dropped in adapter**: `DreamingReport.FactsPruned` is never propagated to `autonomous.DreamReport` (field missing entirely).
3. **No early exit**: The cycle runs all LLM-heavy phases even when there's nothing to do (all facts recently verified, no new signals).

## Changes

### 1. Add missing fields to report structs

**`gateway-go/internal/memory/dreaming.go`** — `DreamingReport`:
- Add `UserModelUpdated int` and `MutualUpdated int`

**`gateway-go/internal/autonomous/dreamer.go`** — `DreamReport`:
- Add `FactsPruned int`, `UserModelUpdated int`, `MutualUpdated int`

**`gateway-go/internal/memory/store.go`** — `DreamingLogEntry`:
- Add `UserModelUpdated int` and `MutualUpdated int`

### 2. Return counts from phases 4 and 5

**`gateway-go/internal/memory/dreaming.go`**:
- `updateUserModel()`: change return from `error` to `(int, error)`, return `len(profile)` (keys updated)
- `userModelPhase.Run()`: capture count, set `s.report.UserModelUpdated`

**`gateway-go/internal/memory/dreaming_mutual.go`**:
- `synthesizeMutualUnderstanding()`: change return from `error` to `(int, error)`, return `updated` count
- `mutualPhase.Run()`: capture count, set `s.report.MutualUpdated`

### 3. Propagate all fields through adapter

**`gateway-go/internal/memory/dreaming_adapter.go`** — `RunDream()`:
- Add `FactsPruned`, `UserModelUpdated`, `MutualUpdated` to the DreamReport conversion

### 4. Update all consumers

**`gateway-go/internal/memory/dreaming.go`** — cycle complete log:
- Add `user_model` and `mutual` to log fields

**`gateway-go/internal/autonomous/service.go`**:
- `runDreamingAsync()` log: add `pruned`, `user_model`, `mutual`
- `notifyDreaming()`: include all 7 metrics; if total work is 0, show "변경 없음" instead

**`gateway-go/internal/memory/store_meta.go`**:
- `InsertDreamingLog()`: add `user_model_updated, mutual_updated` columns
- `LastDreamingLog()`: read the new columns

### 5. Update DB schema

**`gateway-go/internal/unified/schema.go`** — `dreaming_log` CREATE TABLE:
- Add `user_model_updated INTEGER NOT NULL DEFAULT 0`
- Add `mutual_updated INTEGER NOT NULL DEFAULT 0`

**`gateway-go/internal/unified/migrate.go`** (or schema.go init):
- ALTER TABLE dreaming_log ADD COLUMN for existing databases (idempotent, ignore "duplicate column" error)

**`gateway-go/internal/memory/testutil_test.go`**:
- Add new columns to test schema DDL

### 6. Add early exit when no work to do

**`gateway-go/internal/memory/dreaming.go`** — `RunDreamingCycle()`:

After phases 0/0.5/0.75 (cheap cleanup), check if LLM phases would do anything:
```
factsForVerify = len(store.GetFactsForDreaming(ctx))
muSignals = store.GetUserModelEntry(ctx, "mu_signals_raw")
cleanupDid = (report.FactsExpired + report.FactsPruned) > 0

if factsForVerify == 0 && activeCount < 5 && muSignals == "" && !cleanupDid {
    log "aurora-dream: skipping LLM phases, no new data"
    return report early (with cleanup counts)
}
```

This avoids ~8 minutes of LLM calls when there's genuinely nothing to process. The guard is conservative: if cleanup phases changed facts, or if there are unprocessed mutual signals, LLM phases still run.

## Files to modify

| File | Change |
|------|--------|
| `gateway-go/internal/memory/dreaming.go` | Report struct, phase 4 return, early exit, log |
| `gateway-go/internal/memory/dreaming_mutual.go` | Phase 5 return count |
| `gateway-go/internal/autonomous/dreamer.go` | DreamReport struct |
| `gateway-go/internal/memory/dreaming_adapter.go` | Propagate new fields |
| `gateway-go/internal/autonomous/service.go` | Log + notification format |
| `gateway-go/internal/memory/store.go` | DreamingLogEntry struct |
| `gateway-go/internal/memory/store_meta.go` | Insert/read new columns |
| `gateway-go/internal/unified/schema.go` | DDL for new databases |
| `gateway-go/internal/unified/migrate.go` | ALTER TABLE for existing DBs |
| `gateway-go/internal/memory/testutil_test.go` | Test schema DDL |

## Verification

1. `make go` — builds without errors
2. `make go-test` — all existing tests pass (new struct fields zero-value safely)
3. `scripts/dev-live-test.sh restart && scripts/dev-live-test.sh smoke` — gateway starts
4. Wait for or trigger a dream cycle, verify log shows all 7 metrics
5. When no work to do, verify "skipping LLM phases" log and fast completion (<5s)
