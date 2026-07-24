# Even G2 × Deneb

Thin Even Realities G2 surface for Deneb — not a full client.

| Piece | Role |
|---|---|
| Gateway `evenapi` | Even App **Custom AI / Add Agent** → `POST /v1/chat/completions` → Deneb `glasses:main` |
| This folder | Even Hub **Glance** plugin (schedule / urgent / todos HUD) |

Research: `docs/research/even-g2-deneb-integration.md`

## Custom AI (P1 — use this first)

1. Set `DENEB_EVEN_G2_BRIDGE_TOKEN` on the gateway host (see `.env.example`).
2. Expose the gateway on Tailscale (or equivalent private net).
3. In the Even Realities app → Even AI → Add Agent / Custom AI:
   - URL: `http://<tailscale-ip>:<port>/v1/chat/completions`
     (alias: `/api/even/v1/chat/completions`)
   - Token: the same bearer as `DENEB_EVEN_G2_BRIDGE_TOKEN`
4. Ask on the glasses. Short answers render on the HUD; long work acks and continues in the `glasses:main` session.

Do **not** point Custom AI at wormhole — that path is an LLM proxy, not the agent.

## Glance plugin (P2)

```bash
cd even-g2
npm install
npm run dev          # Vite on :5173
# other terminal:
npx evenhub-simulator http://localhost:5173
```

On hardware: enable Even Hub developer mode, then:

```bash
npx evenhub qr --url "http://<lan-ip>:5173"
```

Configure gateway base URL + bridge token once in the plugin (stored in WebView localStorage — not baked into `.ehpk`).

Pack:

```bash
npm run build
npx evenhub pack app.json dist -o deneb-glance.ehpk
```

## Design constraints

- Canvas 576×288, monochrome green text containers (no HTML on glass)
- ~400 character replies
- No secrets inside the packaged `.ehpk`
