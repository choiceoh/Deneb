# Acp

> 144 nodes · cohesion 0.03

## Key Concepts

- **methods_acp_test.go** (28 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **testACPDeps()** (27 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **testACPDispatcher()** (26 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **dispatchACP()** (23 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **NewSubagentCommandDepsFromACP()** (17 connections) — `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring.go`
- **.initACPSubsystem()** (17 connections) — `gateway-go/internal/runtime/server/server_init_acp.go`
- **.SpawnSubagent()** (15 connections) — `gateway-go/internal/pipeline/autoreply/acp/subagent_deps.go`
- **NewACPRegistry()** (13 connections) — `gateway-go/internal/pipeline/autoreply/acp/acp.go`
- **acp.go** (13 connections) — `gateway-go/internal/pipeline/autoreply/acp/acp.go`
- **unmarshalPayload()** (13 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **ACPRegistry** (12 connections) — `gateway-go/internal/pipeline/autoreply/acp/acp.go`
- **.SyncFromService()** (9 connections) — `gateway-go/internal/pipeline/autoreply/acp/acp_persistence.go`
- **SessionBindingService** (9 connections) — `gateway-go/internal/pipeline/autoreply/acp/bindings.go`
- **StartSubagentResultInjection()** (9 connections) — `gateway-go/internal/pipeline/autoreply/acp/context_injection.go`
- **acp_wiring.go** (9 connections) — `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring.go`
- **requireError()** (9 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **.KillSubagent()** (8 connections) — `gateway-go/internal/pipeline/autoreply/acp/subagent_deps.go`
- **TestACPSubagentCommandHandler_Handle()** (8 connections) — `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring_test.go`
- **TestNewSubagentCommandDepsFromACP()** (8 connections) — `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring_test.go`
- **TestACPKill_Success()** (8 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **TestACPStartStop()** (8 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **TestACPWriteOps_DisabledState()** (8 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- **SubagentInfraDeps** (7 connections) — `gateway-go/internal/pipeline/autoreply/acp/subagent_deps.go`
- **bindings.go** (7 connections) — `gateway-go/internal/pipeline/autoreply/acp/bindings.go`
- **requireOK()** (7 connections) — `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- *... and 119 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/pipeline/autoreply/acp/acp.go`
- `gateway-go/internal/pipeline/autoreply/acp/acp_persistence.go`
- `gateway-go/internal/pipeline/autoreply/acp/bindings.go`
- `gateway-go/internal/pipeline/autoreply/acp/context_injection.go`
- `gateway-go/internal/pipeline/autoreply/acp/context_injection_test.go`
- `gateway-go/internal/pipeline/autoreply/acp/registry_persistence.go`
- `gateway-go/internal/pipeline/autoreply/acp/subagent_deps.go`
- `gateway-go/internal/pipeline/autoreply/acp/subagent_deps_test.go`
- `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring.go`
- `gateway-go/internal/pipeline/autoreply/subagent/acp_wiring_test.go`
- `gateway-go/internal/pipeline/autoreply/subagent/facade.go`
- `gateway-go/internal/pipeline/autoreply/subagent/facade_test.go`
- `gateway-go/internal/runtime/rpc/handler/process/process_acp.go`
- `gateway-go/internal/runtime/rpc/methods_acp_test.go`
- `gateway-go/internal/runtime/server/server_init_acp.go`

## Audit Trail

- EXTRACTED: 496 (67%)
- INFERRED: 243 (33%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*