# MCP stdio client

Owns the minimal Model Context Protocol client over stdio JSON-RPC: spawn a
configured server command, settle the protocol era once, then `tools/list` / <!-- docref:ignore -->
`tools/call` / liveness probe. Resources, prompts, and sampling are <!-- docref:ignore -->
intentionally out of scope.

Two eras are spoken, decided per server at initialization. **`initialize` is
always the first thing on the wire**; `server/discover` runs only after it
fails (see the invariants below — the order is a safety property):

| Era | Detection | Per request |
|---|---|---|
| 2024-11-05 … 2025-06-18 | `initialize` succeeds | `initialize` + `notifications/initialized` pinned the version; params unchanged | <!-- docref:ignore -->
| **2026-07-28** ("MCP 2.0") | `initialize` fails, then `server/discover` answers and lists the revision | `params._meta` carries protocolVersion + clientInfo + clientCapabilities; `ping` → `server/discover` | <!-- docref:ignore -->

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
- **Invariant — nothing precedes `initialize`.** A handshake-era server owns a
  lifecycle in which initialize must come first, and some enforce it by
  dropping the transport rather than answering "method not found". On stdio a
  dropped transport is a dead child, so a pre-initialize probe would leave
  nothing to fall back onto — and since every respawn re-runs detection, such
  a server would be permanently unusable. A 2.0 server has no lifecycle to
  violate and must answer an unknown method, so it absorbs a stray
  `initialize` harmlessly. Hence: older era first, always. (The native Kotlin
  client probes in the opposite order on purpose — HTTP has no shared
  transport to lose, and that direction is what the spec recommends there.)
- **Invariant**: both era attempts are detection, not calls — a failure is an
  expected answer, so both go through `probe` (not `roundTrip`) and neither
  lands in `Stats().LastError` while the other era still might answer. The
  failure is counted once, in `handshake`, when both have been ruled out.
  `discover` is bounded to half of `initTimeout` so a server that silently
  drops unknown methods cannot burn the rest of the budget.
- An MRTR `input_required` result is an error, not an empty tool output: this
  client declares no sampling/elicitation/roots capability, so it names the
  capability the server asked for instead of rendering nothing.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/platform/mcpclient
```
