---
title: Page Agent browser
summary: Workstation bridge so Deneb browser tool can drive the user's real Chrome via Page Agent.
read_when:
  - Enabling interactive browser control for logged-in SaaS/SPA pages
  - Setting DENEB_BROWSER_URL on the gateway host
  - Running scripts/dev/page-agent-bridge on a desktop
  - Wiring Amaranth/electronic-approval phone notifications to work-feed summaries
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

## `groupware` tool (전자결재 · 게시판)

API inventory, box codes, HMAC auth, and approve/reject notes:
[groupware-amaranth.md](./groupware-amaranth.md).

**How it works (fast path):** Playwright logs in once and caches `auth_a_token` + `hash_key` in `~/.deneb/groupware-session.json`. Subsequent `list`/`read` call Amaranth internal APIs with HMAC (`wehago-sign`) — typically **tens of ms**, not a full browser scrape.

| action | area | folder | Meaning |
|--------|------|--------|---------|
| `status` | — | — | Credentials configured? |
| `list` | `approval` | `pending`/`done`/`cc`/`total`/`all` | 미결 · 기결 · 수신참조 · 전체결재문서 (list 기본=`all` 순회) |
| `list` | `board` | — | 게시판 최근 글 |
| `read` | `approval`/`board` | (approval) | Match by `query` title/keyword (approval 기본=`pending`) |


## Proactive e-approval (phone → work feed)

Amaranth10 (Douzone) electronic-approval notifications already reach the gateway via
`miniapp.event.ingest` (`DenebNotificationListenerService` → structured
`종류: 전자결재` text).

### Preferred: srv4 headless login

Set credentials on the gateway host — no PC Chrome bridge required:

```bash
export DENEB_GROUPWARE_URL='https://tsgw.topsolar.kr'   # default
export DENEB_GROUPWARE_COMPANY='topsolar'               # default
export DENEB_GROUPWARE_USER='…'
export DENEB_GROUPWARE_PASSWORD='…'
```

Smoke login (does not print the password):

```bash
cd scripts/dev/groupware-reader && npm install
DENEB_GROUPWARE_USER=… DENEB_GROUPWARE_PASSWORD=… npm run login-check
```

When both user and password are set, the gateway runs Playwright headless on srv4
(`scripts/dev/groupware-reader/read.mjs`, same path as the `groupware` tool) before the judgment turn and injects
`[브라우저에서 읽은 결재 본문]`. Approve/reject is never automated.

### Fallback: workstation Page Agent bridge

If credentials are unset (or the headless read fails), the gateway falls back to
`DENEB_BROWSER_URL` / `DENEB_BROWSER_TOKEN` (PC Chrome session).

Tiny-gate is skipped for e-approval events so they are not dropped as routine noise.

## Security

- `/v1/*` requires Bearer or `X-Deneb-Browser-Token`.
- Extension hub WS is localhost-only.
- Do not commit tokens or API keys.

## Limits (Page Agent)

No vision (icon-only buttons), no drag-drop / right-click / shortcuts, limited code editors (Monaco). Better semantic HTML → higher success rate.
