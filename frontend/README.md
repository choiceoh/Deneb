# Deneb Mini App (frontend)

Vanilla TS + Vite. Bundled into the gateway binary via `//go:embed` (see PR-C).

## Development

```bash
# install once
pnpm install

# run with HMR; proxies /api → 127.0.0.1:18790 (dev gateway)
pnpm dev
```

Open http://localhost:5173/app/ — the page will render a "open me from Telegram"
banner because `window.Telegram.WebApp` is absent in a plain browser. For
real-flow testing, expose the dev gateway via Cloudflare Tunnel (see
`docs/operations/cloudflare-tunnel-setup.md` in PR-D) and open the bot's menu
button on a real Telegram client.

For local UI-only checks, append `?mockTelegram=1` on localhost:

```text
http://localhost:5173/app/?mockTelegram=1#/settings
```

The mock is ignored outside localhost/127.0.0.1/[::1].

## Build

```bash
pnpm build
# emits dist/ — copied into gateway-go/internal/runtime/server/miniapp_dist/
# by `make embed-frontend` (PR-C).
```

## Files

- `src/main.ts` — boot: call whoami + ping, render
- `src/rpc.ts` — POST `/api/v1/miniapp/rpc` client, attaches `Authorization: tma <raw>`
- `src/styles.css` — Telegram theme-aware styling
- `index.html` — loads Telegram's WebApp SDK from `telegram.org/js`
- `vite.config.ts` — `base: '/app/'`, dev proxy to gateway

## Why Vanilla TS

The Mini App intentionally stays Vanilla TS while its state remains small and
WebView-focused. Revisit React/Solid only if client-side state grows beyond
the current tab/list/detail/chat surfaces.
