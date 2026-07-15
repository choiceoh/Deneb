---
description: GatewayHub wiring discipline and method registration rules
globs:
  - "gateway-go/internal/runtime/server/method_registry*.go"
  - "gateway-go/internal/runtime/serverwire/**"
  - "gateway-go/internal/runtime/server/gateway_hub.go"
  - "gateway-go/internal/runtime/rpc/rpcutil/gateway_hub.go"
  - "gateway-go/internal/runtime/rpc/handler/**"
---

# GatewayHub Wiring Rules

## Feature composition roots

HTTP/boot stays in `runtime/server`. Feature wiring lives in sibling packages that
depend on `serverport.Host` (never on `runtime/server`):

- `runtime/servermail` — mail/calendar/phone/wiki-mail/workfeed + memory stores
- `runtime/serverchat` — chat pipeline, sessions, cron/hooks, ACP
- `runtime/serverauto` — autonomous tasks, genesis skill lifecycle
- `runtime/serverport` — Host interface (leaf)
- `runtime/serverwire` (+ `early`/`late`/`porttypes`) — RPC Handler Deps assembly


## 5 Rules (enforced by code review + snapshot test)

### Rule 1: No wiring outside `serverwire` (+ thin server boot)

Handler Deps structs are assembled **only** in `internal/runtime/serverwire/early`
and `internal/runtime/serverwire/late`. Server fills a `Ports` bag
(`method_registry_wire.go`) and calls `early.RegisterDomains` /
`late.RegisterDomains`. Capability store init that mutates `Server` stays in
`server/method_registry.go` (`initializeEarlyMethodCapabilities`); observe /
miniapp method bags are built via `early.BuildEarlyCapabilities`.
Exception: `registerBuiltinMethods()` in `server_rpc.go`,
`registerConfigLifecycleMethods()` in `server_rpc_channel.go`, and session
domain registration in `server_rpc_session.go`.

### Rule 2: Hub is built only in `buildHub()`

`server/gateway_hub.go:buildHub()` is the sole constructor (a thin wrapper over
`rpcutil.NewGatewayHub`). Hub fields are private with read-only accessors.
Post-construction mutation is limited to late-bound optional services
(`SetWikiStore`, `SetLocalAIHub`, `SetEmbeddingClient`, `SetContactsStore`,
`SetInsights`). The chat handler lives on `Server` (`s.Chat.ChatHandler` via Ports) and is
passed into handler `Deps` from late registration via `Ports` — there is no
`hub.SetChat`.

### Rule 3: Handlers never import Hub

Handler packages (`internal/runtime/rpc/handler/*`) accept `Deps` structs only.
They must NOT import `rpcutil.GatewayHub` or the `server` package.

### Rule 4: Adding a new handler (3-step process)

1. Add service field to `rpcutil.GatewayHub` (new domains only)
2. Add Deps wiring to `serverwire/early` or `serverwire/late`; extend `Ports`
   groups if a new Server dependency is required (`method_registry_wire.go`)
3. Define `Deps` struct + `Methods(deps Deps)` in the handler package

### Rule 5: No adapter files

Do not create `hub_adapters.go` or similar adapter layers.
Inline Deps literals in `serverwire/early` and `serverwire/late` are the only
wiring point. `Ports` (with `WorkFeed` / `Mail` / `Phone` / `Genesis` groups) is
the explicit bag — not an adapter wrapping Hub.

## Registration Phases

| Phase | Function | Timing | Content |
|---|---|---|---|
| Builtin | `registerBuiltinMethods()` | Before hub | Gateway status (server-state closures) |
| Early | `registerEarlyMethods` → `early.RegisterDomains` | Before chatHandler | ~50 domains via hub inline |
| Session | `registerSessionRPCMethods()` | Creates chatHandler | Chat pipeline init + handler |
| Late | `registerLateMethods` → `late.RegisterDomains` | After chatHandler | Chat / BTW / Miniapp-chat / Exec / Wiki / Models / Genesis / GmailAnalyze |
| Side effects | `registerWorkflowSideEffects(hub)` | After late | Non-RPC: autonomous, dreaming (Aurora), notifier |

## Snapshot Test

`method_registry_test.go:TestMethodRegistry_RequiredMethodsRegistered` verifies
all required RPC methods are registered. When adding/removing methods, update
the `requiredMethods` list.
