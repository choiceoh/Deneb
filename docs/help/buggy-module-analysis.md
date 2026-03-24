---
title: Bug-Prone Module Analysis
summary: Analysis of modules with highest bug risk based on complexity, test gaps, and error patterns.
read_when:
  - You want to identify which modules are most likely to contain bugs
  - You are prioritizing test coverage improvements
  - You are planning refactoring efforts for reliability
---

# Bug-Prone Module Analysis

Analysis of Deneb codebase modules with the highest bug risk, evaluated by code complexity, test coverage gaps, error handling patterns, concurrency risks, and recurring fix history.

## Risk Classification

### CRITICAL (Immediate Attention Required)

#### 1. `src/context-engine/lcm/src/compaction.ts` — 1,701 LOC

- **Risk indicators**: Zero direct test coverage, most algorithmically dense file, 3 silent `catch {}` blocks
- **Bug risks**:
  - **Summary tree corruption**: `compactFullSweep` iterates leaf and condensed passes with depth tracking. If the summary store returns stale data mid-sweep (concurrent session activity), the depth chain becomes inconsistent — a condensed summary may reference source summaries already pruned.
  - **Token counting drift**: `estimateTokens()` approximation errors compound over multi-round compaction (up to `maxRounds: 10`). When real token count exceeds budget after compaction reports success, the context window silently overflows.
  - **Lossy fallback truncation**: `FALLBACK_MAX_CHARS = 512 * 4` truncates deterministically when LLM summarization fails. The 3 silent `catch {}` blocks make failures invisible — the system silently loses conversation context with no signal to the user.
  - Fanout constraints not enforced during concurrent compaction calls
- **Recommendation**: Highest-priority module for test investment. Add integration tests exercising `compactFullSweep` with mock `SummaryStore`/`ConversationStore`, specifically: (a) multi-round escalation, (b) concurrent writes during compaction, (c) token budget adherence post-compaction.

#### 2. `src/context-engine/lcm/src/engine.ts` — 1,498 LOC, 87 async/await

- **Risk indicators**: No tests, most async operations, session queuing logic
- **Bug risks**:
  - **Session queue starvation**: If a compaction operation blocks (e.g., waiting for LLM summarization timeout), queued sessions stall indefinitely. No visible timeout or circuit breaker in the engine queue path.
  - **Subagent lifecycle coupling**: Exposes `onSubagentEnded` (called from `subagent-registry.ts`), creating a cross-module dependency. Errors caught best-effort in `notifyContextEngineSubagentEnded` — context can silently desynchronize from actual agent state.
  - Partial state residue when async operations fail during context orchestration
- **Recommendation**: Pair with `compaction.ts` testing. Add engine-level tests simulating concurrent session access, compaction timeout, and subagent-end notification races.

#### 3. `src/memory/qmd-manager.ts` — 1,903 LOC

- **Risk indicators**: Largest file in codebase, test file exists (2,807 lines) but gaps remain, 20 catch clauses with 3 silent
- **Bug risks**:
  - **Embed queue deadlock**: `qmdEmbedQueueTail` promise-chain lock — if a task throws synchronously before the `try` block, `release()` never fires, permanently deadlocking all subsequent embed operations for the process lifetime.
  - **Silent retry storm**: Retry loop with `QMD_EMBED_BACKOFF_MAX_MS = 1 hour` — a persistent failure silently retries for up to an hour before any user feedback.
  - **Fragile error matching**: `shouldRepairNullByteCollectionError` and `shouldRepairDuplicateDocumentConstraint` use string matching on qmd CLI subprocess error messages. If CLI changes error format, repairs silently stop triggering.
  - FTS rebuild failure with no recovery (recently fixed in #147 — may recur)
- **Recommendation**: Split into focused sub-modules (collection management, embedding lifecycle, search, export). Pin error-message patterns to version-checked constants. Add timeout/circuit-breaker to embed queue.

#### 4. `src/gateway/server-methods/sessions/sessions.ts` — 1,146 LOC

- **Risk indicators**: Core session CRUD path, multiple recent fixes (#131, #133), only 2 try/catch for 20 await calls
- **Bug risks**:
  - **Low error handling ratio**: 20 await calls with only 2 try/catch — most async failures propagate as unhandled RPC errors to clients, potentially exposing internal stack traces.
  - **Session mutation races**: Multiple RPC methods (create, delete, reset, patch, send, abort) callable concurrently for same session. Raw writes could sever the parentId DAG chain.
  - Incomplete recovery path for session file I/O failures
- **Recommendation**: Add session-level locking or queuing for mutating RPC methods. Wrap all RPC handlers in standard error boundary producing safe error shapes.

---

### HIGH (Address Soon)

#### 5. `src/agents/subagent/subagent-registry.ts` — 1,512 LOC, 46 imports

- **Risk indicators**: 7 mutable module-level state declarations (`subagentRuns` Map, `sweeper`, `listenerStarted`, `restoreAttempted`, `resumedRuns` Set, `pendingLifecycleErrorByRunId` Map, `endedHookInFlightRunIds` Set). Uses `var restoreAttempted = false` to work around TDZ/circular-import — fragile initialization order.
- **Bug risks**:
  - **Orphan reconciliation race**: `reconcileOrphanedRestoredRuns` iterates `subagentRuns` while `completeSubagentRun` concurrently mutates the same Map. `schedulePendingLifecycleError` timer callback reads from `subagentRuns` without holding any lock.
  - **Announce retry exhaustion**: `MAX_ANNOUNCE_RETRY_COUNT = 3` (fix for #18264) means transient network failures permanently drop completion notifications. Combined with `ANNOUNCE_EXPIRY_MS = 5 min`, a subagent completing during a brief gateway hiccup may never deliver its result.
  - **Disk persistence without validation**: `persistSubagentRunsToDisk` serializes entire Map — if a run is in an inconsistent intermediate state, restored state on next startup is corrupt. `restoreSubagentRunsFromDisk` has no schema validation.
- **Recommendation**: Extract mutable state into a class instance for testability/resettability. Add restore-path validation. Test announce-retry/expiry interaction specifically.

#### 6. `src/agents/pi-embedded-runner/run/attempt.ts` — 1,464 LOC, 67 imports

- **Risk indicators**: Highest import count in codebase (67), coupled to nearly every subsystem. Test file exists (1,109 lines) but coverage incomplete.
- **Bug risks**:
  - **Import fan-out fragility**: Coupled to sandbox, tools, sessions, models, skills, media, streaming, transcript repair. A breaking change in any dependency silently affects agent execution.
  - **Reactive repair pattern**: Imports `sanitizeToolUseResultPairing` and `repairSessionFileIfNeeded` — existence of these indicates known transcript corruption patched reactively, not prevented. New corruption modes manifest as silent conversation breakage.
  - **Session write lock stall**: `acquireSessionWriteLock` hold time derived from agent timeout (potentially very long). Other operations waiting for this lock stall indefinitely.
  - Bootstrap context + tail protection budget calculation failure
- **Recommendation**: Factor out sub-orchestrators (tool execution, session setup, streaming) to reduce coupling. Replace repair imports with prevention at the write layer.

#### 7. `src/acp/control-plane/manager.core.ts` — 1,421 LOC

- **Risk indicators**: Multiple concurrent data structures (Map, Queue, Cache). Test file exists (1,661 lines) but concurrency model hard to test exhaustively.
- **Bug risks**:
  - **activeTurn identity race**: Reference equality check at line ~735 (`this.activeTurnBySession.get(actorKey) === activeTurn`) guards cleanup. If a new turn is created for the same actor key between set and cleanup check, old turn's resources (abort controllers, timers) may never be cleaned up.
  - **Idle eviction TOCTOU**: `evictIdleRuntimeHandles` checks `activeTurnBySession.has(candidate.actorKey)` — a turn could start between the check and the eviction.
  - **Concurrent session limit bypass**: `enforceConcurrentSessionLimit` called inside `withSessionActor` operates on global state. Multiple concurrent `initializeSession` calls for different actor keys could each pass the limit check before any completes.
- **Recommendation**: Add targeted concurrency tests using Promise scheduling for activeTurn identity guard and idle eviction race. Consider single coordination lock for limit enforcement.

#### 8. `src/security/audit-extra.async.ts` — 1,264 LOC

- **Risk indicators**: Security-critical audit scanning, no tests, 4 silent `catch {}` blocks
- **Bug risks**:
  - **False negatives from swallowed errors**: Silent catch blocks mean filesystem permission errors, Docker connectivity issues, or skill scanner failures result in a clean audit report rather than a flagged finding. A failed security check that silently succeeds is worse than a crash.
  - **Permanently cached failed imports**: `skillsModulePromise` and `configModulePromise` cache module imports globally. If the first import fails (transient error), the failed promise is cached permanently.
- **Recommendation**: Replace all silent catches with `catch` blocks that emit audit findings of type "scan-error". Add null-check or retry logic for cached module promises.

#### 9. `src/plugins/registry.ts` — 1,008 LOC, 38 imports

- **Risk indicators**: Plugin lifecycle hub, no tests
- **Bug risks**:
  - Data loss when hook result mutation strips legacy fields
  - HTTP path normalization edge cases causing route conflicts
  - Unclear command registration failure propagation
- **Recommendation**: Plugin register/unregister cycle tests, route conflict tests

#### 10. `src/plugins/loader.ts` — 727 LOC

- **Risk indicators**: Dynamic module loading (Jiti), no cache invalidation
- **Bug risks**:
  - Cached registry not refreshed on config changes
  - Jiti alias resolution fallback loading wrong module version
  - Circular import not detected
- **Recommendation**: Hot-reload cache invalidation verification tests

---

### MEDIUM (Monitor)

| Module                                                | LOC   | Primary Risk                                       |
| ----------------------------------------------------- | ----- | -------------------------------------------------- |
| `src/agents/subagent/subagent-announce.ts`            | 1,509 | Delivery/retry logic edge cases                    |
| `src/agents/pi-embedded-runner/compact.ts`            | 1,172 | Pi session compaction, 25 try/catch                |
| `src/gateway/server.impl.ts`                          | 1,044 | 76 imports, gateway orchestrator                   |
| `src/gateway/server/ws-connection/message-handler.ts` | 1,114 | WebSocket auth/device pairing                      |
| `src/config/zod-schema.ts`                            | 1,034 | Config validation, all user configs depend on this |
| `src/infra/state-migrations.ts`                       | 967   | DB migration rollback difficulty                   |
| `src/channels/plugins/setup-wizard.ts`                | 864   | 50 async vs 2 try/catch (imbalance)                |
| `src/markdown/ir.ts`                                  | 973   | Markdown IR transformation                         |
| `src/acp/translator.ts`                               | 1,099 | ACP protocol translation layer                     |

---

## Cross-Cutting Concerns

### Concurrency and Shared Mutable State

The codebase makes heavy use of module-level `Map` and `Set` instances as shared state (`subagentRuns`, `activeTurnBySession`, `pendingLifecycleErrorByRunId`, `qmdEmbedQueueTail`). These are accessed from multiple async code paths without formal synchronization beyond promise-chain queuing.

- **SessionActorQueue**: Serializes per-actor, not globally. `pendingBySession` Map sync missing, deadlock if `onSettle` callback fails.
- **RuntimeCache**: get-check-set not atomic, no safety guarantees during eviction.
- **WebSocket auth**: Concurrent connection state conflicts during device pairing.

**Impact**: Race conditions manifest as silent data loss (dropped announcements, stale cache entries) rather than crashes, making them difficult to detect and reproduce.

### Error Handling Polarization

Two opposite anti-patterns coexist:

- **Over-defensive**: `qmd-manager.ts` (25 try/catch wrapping nearly every operation) masks root causes, makes debugging difficult.
- **Under-defensive**: `message-handler.ts` (1 try/catch for 1,114 LOC of untrusted network input) and `sessions.ts` (2 for 1,146 LOC) leave most async paths unguarded.
- **Silent catch `{}`**: Found across critical paths — `compaction.ts` (3), `audit-extra.async.ts` (4), `qmd-manager.ts` (3). These produce false successes in security scans and invisible context loss.

**Recommended standard**: (1) RPC/WebSocket handlers get mandatory top-level error boundary with safe client-facing error shapes. (2) Internal modules use targeted try/catch only where recovery is possible. (3) Ban bare `catch {}` — require `catch { /* intentional: <reason> */ }` with mandatory comment.

### Test Coverage Gaps

| Area           | Test Ratio | Untested Large Files |
| -------------- | ---------- | -------------------- |
| Gateway        | 43%        | 12 (>500 LOC)        |
| Context Engine | ~28%       | 11 (>500 LOC)        |
| Plugins        | 53%        | 5 (>500 LOC)         |
| Channels       | 60%        | 5 (>500 LOC)         |

### Recurring Fix Patterns (Git History)

- **Chat streaming/text retention** — 7+ fixes
- **Async/concurrency** (input swallowing, output loss) — 4+ fixes
- **Module boundaries** (lazy loading) — 3+ fixes
- **Data handling** (empty-text filtering, null bytes) — 3+ fixes

---

## Prioritized Action Items

| Priority | Action                                                         | Modules                                                   | Expected Impact                                                      |
| -------- | -------------------------------------------------------------- | --------------------------------------------------------- | -------------------------------------------------------------------- |
| P0       | Add compaction integration tests                               | `compaction.ts`, `engine.ts`                              | Prevents silent context loss — the most user-visible bug category    |
| P0       | Add error boundary to WS message handler                       | `message-handler.ts`                                      | Prevents connection crashes from malformed input                     |
| P1       | Extract subagent registry mutable state into testable class    | `subagent-registry.ts`                                    | Enables direct testing, eliminates `var` TDZ workaround              |
| P1       | Replace silent `catch {}` with logged/finding-emitting catches | `audit-extra.async.ts`, `compaction.ts`, `qmd-manager.ts` | Eliminates false-negative security audits and invisible context loss |
| P1       | Add embed queue timeout/circuit-breaker                        | `qmd-manager.ts`                                          | Prevents permanent deadlock and hour-long silent retry storms        |
| P2       | Add session-method-level error boundaries                      | `sessions.ts`                                             | Standardizes RPC error responses, prevents stack trace leaks         |
| P2       | Add concurrency tests for ACP manager                          | `manager.core.ts`                                         | Catches activeTurn identity and eviction TOCTOU races                |
| P2       | Split `qmd-manager.ts` below 700 LOC                           | `qmd-manager.ts`                                          | Reduces merge conflicts, enables focused testing                     |
| P3       | Reduce `attempt.ts` import fan-out                             | `attempt.ts`                                              | Reduces coupling surface and breakage risk                           |
| P3       | Add persistent outbox for subagent announcements               | `subagent-announce.ts`                                    | Prevents dropped completion notifications during gateway hiccups     |
