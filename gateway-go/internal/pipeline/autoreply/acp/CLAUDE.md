# ACP (Agent Control Protocol) session map

Owns ACP session registry, bindings persistence, prompt translation, and
subagent spawn deps for external ACP clients.

## Entry points

- `acp.go` — `NewACPRegistry`, `ACPRegistry`, `NewACPProjector`
- `acp_translator.go` — `NewACPTranslator`, `IsACPSession`,
  `TranslateStopReason`, `TranslateACPStopReasonToStatus`
- `acp_persistence.go` — `NewBindingStore`, `DefaultBindingStorePath`
- `bindings.go` — `SessionBindingService`, `SessionBindingEntry`
- `subagent_deps.go` — `SubagentInfraDeps`, `SpawnSubagentParams`
- `context_injection.go` — ACP context injection helpers

## Dependency direction and invariants

- **Dependency / port boundary**: translate between ACP wire shapes and
  `domain/session` run status — do not import `runtime/server` method
  registry or chat `run_*` orchestration. Persistence follows the same
  atomic-write pattern as cron stores.
- **Invariant**: binding store writes must be atomic; `IsACPSession` is the
  only gate for ACP-specific delivery; stop-reason translation must round-
  trip without inventing unknown statuses; never leak internal session keys
  into ACP resource URIs incorrectly.
- Registry and binding services stay the single source for active ACP
  sessions.

## Local change scope

ACP protocol glue stays in `autoreply/acp`.

- May co-change: autoreply delivery adapters and session status enums.
- Do not touch: wiki Store, genesis, or tool registration.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/autoreply/acp
```
