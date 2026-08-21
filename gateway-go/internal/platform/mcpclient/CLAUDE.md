# MCP stdio client

Owns the minimal Model Context Protocol client over stdio JSON-RPC: spawn a
configured server command, settle the protocol era once, then `tools/list` / <!-- docref:ignore -->
`tools/call` / liveness probe. Resources, prompts, and sampling are <!-- docref:ignore -->
intentionally out of scope.

Two eras are spoken, decided per server at initialization:

| Era | Detection | Per request |
|---|---|---|
| **2026-07-28** ("MCP 2.0") | `server/discover` answers and lists the revision | `params._meta` carries protocolVersion + clientInfo + clientCapabilities; `ping` → `server/discover` | <!-- docref:ignore -->
| 2024-11-05 … 2025-06-18 | `server/discover` is "method not found" | `initialize` + `notifications/initialized` pinned the version; params unchanged | <!-- docref:ignore -->

## Entry points

- `client.go` — `New`, `Client`, `Start`, `ListTools`, `CallTool`, `Ping`,
  `Close`, `CloseProcess`, `ServerInfo`; `handshake` picks the era
- `stateless.go` — MCP 2026-07-28: `discover` probe, `_meta` construction,
  negotiated-protocol accessor
- `stats.go` — `Stats` (spawn/call counters + last error + recent stderr)
- `render.go` — tool-result rendering into agent-readable text
- `env.go` — child environment filtering for spawned servers

## Dependency direction and invariants

- **Dependency / boundary**: leaf platform package — must not import chat,
  RPC handlers, or domain stores. Consumers (external MCP runtime, tools)
  inject argv + logger.
- **Invariant**: one `Client` is shared; the handshake runs once in the
  background and waiters cancel on their own ctx (never block forever on a
  mutex). Child runs in its own process group with tiered teardown
  (stdin close → SIGTERM group → grace → SIGKILL).
- Server-initiated requests answer "method not found"; fold recent stderr into
  init errors so first-run OAuth URLs surface to operators.
- **Invariant**: the `server/discover` probe is detection, not a call — its <!-- docref:ignore -->
  failure is the expected answer from a handshake-era server, so it goes
  through `probe` (not `roundTrip`) and never lands in `Stats().LastError`.
  It is bounded to half of `initTimeout` so a server that silently drops
  unknown methods cannot starve the initialize fallback.
- An MRTR `input_required` result is an error, not an empty tool output: this
  client declares no sampling/elicitation/roots capability, so it names the
  capability the server asked for instead of rendering nothing.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/platform/mcpclient
```
