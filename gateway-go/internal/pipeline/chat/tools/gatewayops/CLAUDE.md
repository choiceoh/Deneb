# Gatewayops (gateway / heartbeat_update)

Owns agent-facing gateway self-management and HEARTBEAT.md updates. Parent
`runtimeops` no longer implements this surface.

## Entry points

- `gateway.go` — `ToolGatewayWithDeps`, approval token registry, dotted config
- `heartbeat.go` — `ToolHeartbeatUpdate`

Wiring stays in `toolwire/ops.RegisterRuntimeOps` / `RegisterHeartbeatTool`.

## Dependency direction and invariants

- May import `toolport`, `config`, `atomicfile`, `jsonutil`. Must not import
  parent `runtimeops` or `pipeline/chat`.
- Destructive actions (restart/update/config_set) require a single-use,
  payload-bound approval token. Secret paths are refused.
- Approval consume is exactly-once; concurrent consume must succeed once.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/gatewayops
```
