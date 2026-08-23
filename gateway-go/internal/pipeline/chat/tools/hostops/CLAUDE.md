# Hostops (browser / fleet / solarflow / workstation / computer)

Owns agent-facing wrappers for external hosts and the desktop workstation.
Parent `runtimeops` is now exec/process only.

## Entry points

- `browser.go` — `ToolBrowser` (Page Agent bridge; client lives in
  `platform/browserbridge`)
- `fleet.go` — `ToolFleet` (SparkFleet control plane)
- `solarflow.go` — `ToolSolarflow` (read-only ERP analytics)
- `workstation.go` — `ToolWorkstation`, `WorkstationCommandFunc`
- `computer.go` — `ToolComputer`, `ComputerCommandFunc` (host-OS computer use
  via the Andromeda shell; transport + screenshot→text in
  `runtime/server/server_computer.go`)

Wiring stays in `toolwire/ops.RegisterRuntimeOps` / `RegisterWorkstationTool` / `RegisterComputerTool`.

## Dependency direction and invariants

- May import `toolport`, `tooldeps`, `platform/browserbridge`,
  `platform/solarflow`. Must not import parent `runtimeops` or `pipeline/chat`.
- Unconfigured browser/fleet integrations return a calm "off" message.
- Solarflow never mutates. Workstation verbs are arrangement-only. Computer
  verbs are an allowlist with bounded arguments; the desktop re-validates.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/hostops
```
