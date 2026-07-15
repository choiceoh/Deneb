---
title: Page Agent browser
summary: Workstation bridge so Deneb browser tool can drive the user's real Chrome via Page Agent.
read_when:
  - Enabling interactive browser control for logged-in SaaS/SPA pages
  - Setting DENEB_BROWSER_URL on the gateway host
  - Running scripts/dev/page-agent-bridge on a desktop
---

# Page Agent browser

Deneb's `browser` tool drives the user's **real Chrome** through a workstation bridge.

The `web` tool (server HTTP fetch) cannot handle SPAs or logged-in sessions. This bridge attaches to the [Page Agent](https://github.com/alibaba/page-agent) Chrome extension hub so the agent can click and type in tabs the user already authenticated.

## Layout

```
Deneb gateway (gateway host)  --HTTP+token-->  page-agent-bridge (workstation)
                                                      │ WS (loopback only)
                                                      ▼
                                            Page Agent Chrome extension hub
```

## Workstation setup

1. Install [Page Agent Ext](https://chromewebstore.google.com/detail/page-agent-ext/akldabonmimlicnjlflnapfeklbfemhj) in Chrome.
2. Choose a shared token (same value on the gateway).
3. Start the bridge:

```bash
cd scripts/dev/page-agent-bridge
npm install
DENEB_BROWSER_TOKEN='…' \
  LLM_BASE_URL='https://…' LLM_API_KEY='…' LLM_MODEL_NAME='…' \
  npm start
```

- `HOST` defaults to `0.0.0.0` so the gateway host can reach it over Tailscale.
- Hub WebSocket accepts **loopback only**.
- On start, the local browser opens `http://127.0.0.1:38401/` (launcher) so the extension can attach its hub tab.

Optional `LLM_*` env vars are forwarded to the Page Agent hub (OpenAI-compatible).

## Gateway setup

```bash
export DENEB_BROWSER_URL='http://<workstation-tailscale-ip>:38401'
export DENEB_BROWSER_TOKEN='…'   # same token as the bridge
```

After restart, the agent activates the deferred `browser` tool via `fetch_tools`.

| action | Meaning |
|--------|---------|
| `status` | Hub connected / busy |
| `execute` | Natural-language `task` (blocking, up to ~10 min) |
| `stop` | Cancel the running task |

When unset, `browser` returns an "integration off" message (same pattern as `fleet`).

## Security

- `/v1/*` requires Bearer or `X-Deneb-Browser-Token`.
- Extension hub WS is localhost-only.
- Do not commit tokens or API keys.

## Limits (Page Agent)

No vision (icon-only buttons), no drag-drop / right-click / shortcuts, limited code editors (Monaco). Better semantic HTML → higher success rate.
