# MCP stdio client

Owns the minimal Model Context Protocol client over stdio JSON-RPC: spawn a
configured server command, handshake once, then `tools/list` / `tools/call` / <!-- docref:ignore -->
`ping`. Resources, prompts, and sampling are intentionally out of scope.

## Entry points

- `client.go` — `New`, `Client`, `Start`, `ListTools`, `CallTool`, `Ping`,
  `Close`, `CloseProcess`, `ServerInfo`
- `stats.go` — `Stats` (spawn/call counters + last error + recent stderr)
- `render.go` — tool-result rendering into agent-readable text
- `env.go` — child environment filtering for spawned servers

## Dependency direction and invariants

- **Dependency / boundary**: leaf platform package — must not import chat,
  RPC handlers, or domain stores. Consumers (external MCP runtime, tools)
  inject argv + logger.
- **Invariant**: one `Client` is shared; initialize handshake runs once in the
  background and waiters cancel on their own ctx (never block forever on a
  mutex). Child runs in its own process group with tiered teardown
  (stdin close → SIGTERM group → grace → SIGKILL).
- Server-initiated requests answer "method not found"; fold recent stderr into
  init errors so first-run OAuth URLs surface to operators.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/platform/mcpclient
```
