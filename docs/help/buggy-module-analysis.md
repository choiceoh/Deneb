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

#### 1. `src/memory/qmd-manager.ts` — 1,903 LOC

- **Risk indicators**: Largest file in codebase, 127 try/catch blocks (most), no dedicated test file
- **Bug risks**:
  - Data loss during SQLite schema migrations
  - `shouldRepairDuplicateDocumentConstraint` — edge cases in duplicate document constraint repair logic
  - Incomplete null-byte collection error recovery path
  - FTS (Full-Text Search) rebuild failure with no recovery (recently fixed in #147 — may recur)
  - Many of 127 try/catch blocks silently swallow errors
- **Recommendation**: Add integration tests per database repair path, audit silent catches

#### 2. `src/context-engine/lcm/src/compaction.ts` — 1,701 LOC

- **Risk indicators**: No tests, complex token-based compaction algorithm, multi-level escalation
- **Bug risks**:
  - Token counting mismatch leading to memory overflow
  - Summary tree depth calculation errors causing tree corruption
  - Unclear error recovery during `normal` → `aggressive` → `fallback` escalation
  - Fanout constraints not enforced during concurrent compaction calls
- **Recommendation**: Add token boundary tests, escalation level transition tests

#### 3. `src/context-engine/lcm/src/engine.ts` — 1,498 LOC, 87 async/await

- **Risk indicators**: No tests, most async operations, session queuing logic
- **Bug risks**:
  - Session queue contention (race condition)
  - Partial state residue when async operations fail during context orchestration
- **Recommendation**: Add session queuing concurrency tests, failure recovery path tests

#### 4. `src/gateway/server-methods/sessions/sessions.ts` — 1,146 LOC, 58 try/catch

- **Risk indicators**: Core session CRUD path, multiple recent fixes (#131, #133)
- **Bug risks**:
  - Race condition during concurrent session create/delete/reset
  - parentId chain corruption leading to transcript integrity violation
  - Incomplete recovery path for session file I/O failures
- **Recommendation**: Add concurrent session mutation sequence integration tests

---

### HIGH (Address Soon)

#### 5. `src/agents/subagent/subagent-registry.ts` — 1,512 LOC, 46 imports

- **Risk indicators**: Complex state machine, orphan subagent reconciliation logic
- **Bug risks**:
  - `reconcileOrphanedRunsImpl` — may miss runs in transition
  - Retry delay calculation infinite loop or skip
  - No consistency validation for disk-restored state
  - `emitSubagentEndedHookOnce` — hook firing before cleanup causes duplicate processing
- **Recommendation**: State machine transition tests, orphan reconciliation integration tests

#### 6. `src/agents/pi-embedded-runner/run/attempt.ts` — 1,464 LOC

- **Risk indicators**: Core LLM execution orchestration, tool execution and result handling
- **Bug risks**:
  - PI SessionManager appendMessage parentId chain integrity violation
  - `sanitizeToolUseResultPairing` edge case misses
  - Bootstrap context + tail protection budget calculation failure
  - Incomplete state cleanup during failover stream/timeout
- **Recommendation**: Message ordering integrity tests, budget boundary tests

#### 7. `src/acp/control-plane/manager.core.ts` — 1,421 LOC

- **Risk indicators**: Multiple concurrent data structures (Map, Queue, Cache)
- **Bug risks**:
  - `activeTurnBySession` set/delete non-atomic — race condition
  - Runtime cache eviction during active turns — orphaned state
  - SessionActorQueue deadlock on error
  - TTL-based idle cleanup conflicting with active turns
- **Recommendation**: Add concurrency guards, apply atomic operations to Map-based state

#### 8. `src/security/audit-extra.async.ts` — 1,264 LOC, 74 try/catch

- **Risk indicators**: Security-critical async audit logging, no tests
- **Bug risks**: Async audit log loss, security event loss due to race conditions
- **Recommendation**: Async safety tests, audit log verification per error path

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

| Module | LOC | Primary Risk |
|--------|-----|-------------|
| `src/agents/subagent/subagent-announce.ts` | 1,509 | Delivery/retry logic edge cases |
| `src/agents/pi-embedded-runner/compact.ts` | 1,172 | Pi session compaction, 25 try/catch |
| `src/gateway/server.impl.ts` | 1,044 | 76 imports, gateway orchestrator |
| `src/gateway/server/ws-connection/message-handler.ts` | 1,114 | WebSocket auth/device pairing |
| `src/config/zod-schema.ts` | 1,034 | Config validation, all user configs depend on this |
| `src/infra/state-migrations.ts` | 967 | DB migration rollback difficulty |
| `src/channels/plugins/setup-wizard.ts` | 864 | 50 async vs 2 try/catch (imbalance) |
| `src/markdown/ir.ts` | 973 | Markdown IR transformation |
| `src/acp/translator.ts` | 1,099 | ACP protocol translation layer |

---

## Cross-Cutting Concerns

### Concurrency Risks

- **SessionActorQueue**: `pendingBySession` Map sync missing, deadlock if `onSettle` callback fails
- **RuntimeCache**: get-check-set not atomic, no safety guarantees during eviction
- **WebSocket auth**: Concurrent connection state conflicts during device pairing

### Error Handling Anti-Patterns

- **Silent catch**: 67 files swallow errors (no logging/re-throw)
  - High risk: `src/agents/pi-embedded-runner/run/attempt.ts`, `src/media/host.ts`, `src/browser/*.ts`
- **Over-defensive**: `qmd-manager.ts` (127), `audit-extra.async.ts` (74) — lacks error abstraction
- **Under-defensive**: `setup-wizard.ts` — 50 async/await vs only 2 try/catch

### Test Coverage Gaps

| Area | Test Ratio | Untested Large Files |
|------|-----------|---------------------|
| Gateway | 43% | 12 (>500 LOC) |
| Context Engine | ~28% | 11 (>500 LOC) |
| Plugins | 53% | 5 (>500 LOC) |
| Channels | 60% | 5 (>500 LOC) |

### Recurring Fix Patterns (Git History)

- **Chat streaming/text retention** — 7+ fixes
- **Async/concurrency** (input swallowing, output loss) — 4+ fixes
- **Module boundaries** (lazy loading) — 3+ fixes
- **Data handling** (empty-text filtering, null bytes) — 3+ fixes

---

## Prioritized Recommendations

### P0 (Immediate)

1. `src/memory/qmd-manager.ts` — Integration tests per DB repair path, silent catch audit
2. `src/context-engine/lcm/src/compaction.ts` — Token boundary + escalation transition tests
3. `src/gateway/server-methods/sessions/sessions.ts` — Concurrent session mutation sequence tests

### P1 (Soon)

4. `src/acp/control-plane/manager.core.ts` — Add atomic operation guards to Map-based state
5. `src/agents/pi-embedded-runner/run/attempt.ts` — parentId chain integrity verification tests
6. `src/security/audit-extra.async.ts` — Async audit safety tests

### P2 (Planned)

7. Codebase-wide audit of silent catch patterns (67 files)
8. `setup-wizard.ts` error handling reinforcement (resolve async/try-catch imbalance)
9. Test coverage expansion campaign for 500+ LOC files
