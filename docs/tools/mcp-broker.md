---
title: "Personal MCP broker"
description: "Expose the gateway's read-only MCP tool surface to tailnet devices (Cursor, other MCP clients) without opening the gateway itself"
---

# Personal MCP broker (DENEB_MCP_TOKEN)

The gateway serves MCP 2.0 (2026-07-28) on `POST /mcp` with a **read-only
allowlist** — `wiki_search`, `wiki_read`, `wiki_list`, `project_digests`,
`diary_recent`, `calendar_upcoming`, `search_all` — each declaratively mapped
onto `miniapp.*` methods (see `gateway-go/internal/runtime/mcpapi/handler.go`).
This guide covers giving that surface its own credential and exposing it to
your tailnet so any MCP client (Cursor, Claude Desktop, another agent box)
can search your personal knowledge — without ever handing out the
full-privileged native client token.

## 1. Mint a dedicated broker token

The token is just a long random string you generate and store:

```bash
openssl rand -hex 32
```

Set it where the gateway runs (srv4):

```bash
# /etc/deneb environment or the systemd unit
DENEB_MCP_TOKEN=<the hex string>
```

When set, `/mcp` accepts **either** this broker token **or** the normal
client token, both via the `X-Deneb-Client-Token` header. When unset, the
status quo holds (client token only). Revoking the broker token is
independent: change the env and restart; the native clients never notice.

## 2. Expose loopback /mcp to the tailnet only

The gateway keeps its loopback bind — the exposure happens at the tunnel
layer, on the same host:

```bash
# tailnet-only (recommended): your devices, TLS via tailnet certs
tailscale serve --bg --https=443 http://127.0.0.1:18789/mcp
```

Do **not** use `tailscale funnel` (that publishes to the public internet) —
the single-user trust model ends at the tailnet.

## 3. Point an MCP client at it

Cursor (or any MCP 2.0 client) on a tailnet device:

```json
{
  "mcpServers": {
    "deneb": {
      "url": "https://<your-tailnet-host>/mcp",
      "headers": { "X-Deneb-Client-Token": "<broker token>" }
    }
  }
}
```

Quick check from any tailnet device:

```bash
curl -s https://<your-tailnet-host>/mcp \
  -H "X-Deneb-Client-Token: <broker token>" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Guardrails

- **Read-only forever.** Adding a WRITE tool to `mcpTools` is a security
  decision, not a mapping edit (`handler.go` header comment).
- **Kill switch:** `DENEB_MCP_DISABLE=1` removes the endpoint entirely.
- **Scope:** tailnet devices only; the gateway bind never leaves loopback
  (docs/research/improvement-ideas.md §8 scoped exception).
