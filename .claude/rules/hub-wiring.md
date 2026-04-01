---
description: GatewayHub wiring discipline and method registration rules
globs:
  - "gateway-go/internal/server/method_registry.go"
  - "gateway-go/internal/server/gateway_hub.go"
  - "gateway-go/internal/rpc/rpcutil/gateway_hub.go"
  - "gateway-go/internal/rpc/handler/**"
---

# GatewayHub Wiring Rules

## 5 Rules (enforced by code review + snapshot test)

### Rule 1: No wiring outside `method_registry.go`

Handler Deps structs are assembled **only** in `method_registry.go`.
No other file may construct or wire Deps structs for handler registration.
Exception: `registerBuiltinMethods()` in `server_rpc.go` (server-state closures).

### Rule 2: Hub is built only in `buildHub()`

`server/gateway_hub.go:buildHub()` is the sole constructor.
No other file may assign Hub fields (except `hub.Chat` late-bind in `registerLateMethods`
and `hub.Telegram` in `registerEarlyMethods` after plugin creation).

### Rule 3: Handlers never import Hub

Handler packages (`internal/rpc/handler/*`) accept `Deps` structs only.
They must NOT import `rpcutil.GatewayHub` or the `server` package.

### Rule 4: Adding a new handler (3-step process)

1. Add service field to `rpcutil.GatewayHub`
2. Add Deps wiring to `method_registry.go` (registerEarlyMethods or registerLateMethods)
3. Define `Deps` struct + `Methods(deps Deps)` in the handler package

### Rule 5: No adapter files

Do not create `hub_adapters.go` or similar adapter layers.
Inline Deps literals in `method_registry.go` are the only wiring point.

## Registration Phases

| Phase | Function | Timing | Content |
|---|---|---|---|
| Builtin | `registerBuiltinMethods()` | Before hub | Gateway status (server-state closures) |
| Early | `registerEarlyMethods(hub)` | Before chatHandler | ~30 domains via hub inline |
| Session | `registerSessionRPCMethods()` | Creates chatHandler | Chat pipeline init + handler |
| Late | `registerLateMethods(hub)` | After chatHandler | Chat/BTW/Exec/Aurora (~4 domains) |
| Side effects | `registerWorkflowSideEffects(hub)` | After late | Non-RPC: autonomous, dreaming, notifier |

## Snapshot Test

`method_registry_test.go:TestMethodRegistry_RequiredMethodsRegistered` verifies
all required RPC methods are registered. When adding/removing methods, update
the `requiredMethods` list.
