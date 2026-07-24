# Even G2 × Deneb

Thin Even Realities G2 surface for Deneb — not a full client.

| Piece | Role |
|---|---|
| Gateway `evenapi` | Custom AI `POST /v1/chat/completions` → `glasses:main` |
| Gateway glance | `GET /api/even/glance` → 일정/긴급(workfeed)/할 일 2~4줄 |
| This folder | Even Hub **Glance** plugin (notification-first; cal/todo secondary) |

Research: `docs/research/even-g2-deneb-integration.md`

## Custom AI (P1)

1. Set `DENEB_EVEN_G2_BRIDGE_TOKEN` on the gateway host (see `.env.example`).
2. Expose the gateway on Tailscale (or equivalent private net).
3. Even Realities app → Even AI → Add Agent / Custom AI:
   - URL: `http://<tailscale-ip>:<port>/v1/chat/completions`
   - Token: same bearer as `DENEB_EVEN_G2_BRIDGE_TOKEN`

## Glance plugin (P2)

### Dev + QR seed (recommended — token not baked into .ehpk)

```bash
cd even-g2
npm install
npm run dev   # :5173

# other terminal — build a seed, then QR the seeded URL
export DENEB_EVEN_G2_BASE_URL=http://100.x.x.x:18789
export DENEB_EVEN_G2_BRIDGE_TOKEN=…
SEED=$(npm run -s seed)
npx evenhub qr --url "http://<lan-ip>:5173/?seed=${SEED}"
```

Scan with Even Hub developer mode. First launch persists settings to WebView localStorage.

Also supported: `?baseUrl=…&token=…` (or `deneb_base` / `deneb_token`).

### Private build (.ehpk)

```bash
export DENEB_EVEN_G2_BASE_URL=http://100.x.x.x:18789
export DENEB_EVEN_G2_BRIDGE_TOKEN=…
npm run pack   # prepare:config → build → evenhub pack
```

`prepare:config` writes `public/runtime-config.json` and adds the gateway origin to `app.json` network whitelist.

Upload the `.ehpk` through the Even Hub developer portal as a **private** build. Do not submit a build that embeds a production token to the public catalog.

### Simulator

```bash
npm run prepare:config   # optional, for local gateway
npm run dev
npx evenhub-simulator http://localhost:5173
```

### In-glass controls

| 입력 | 동작 |
|---|---|
| 탭 | Glance 새로고침 (`?fresh=1`) |
| 스와이프 ↓ | `/api/even/status` (브리지/챗 준비 상태) |
| 스와이프 ↑ | 이전 페이지 |
| 더블탭 | 종료 확인 |

QR 시드의 `?seed=` 쿼리는 저장 직후 URL에서 제거됩니다 (토큰이 주소창에 남지 않게).

## Notification HUD (P0)

Gateway proactive pushes are normalized to one-line HUD grammar before SSE/FCM:

- title ≤ ~18 runes (e.g. `Deneb · 업무`)
- body single line ≤ ~100 runes, markdown/URLs stripped

### Even 알림 필터 + 측정

1. Phone Even app → Notifications: allow Deneb / FCM channel only (or the app that surfaces Deneb pushes).
2. For one workday, tally **폰을 꺼보지 않고** 안경 HUD만으로 처리한 알림 횟수 (`HUD_OK`) vs 폰을 연 횟수 (`PHONE_OPEN`).
3. Success signal: `HUD_OK / (HUD_OK + PHONE_OPEN)` rising across a few days.
4. Optional diary line: `wiki log` — `G2 HUD: OK=N phone=M`.

## Design constraints

- Canvas 576×288, monochrome green text containers
- Glance is structured data (no agent turn); Custom AI still uses chat sync
- Secrets: prefer QR seed; never commit `runtime-config.json` or `.ehpk`
