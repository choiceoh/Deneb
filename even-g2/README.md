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

패키지명은 **스코프가 붙는다** — `npx evenhub-simulator`(스코프 없음)는 npm에 존재하지 않는다.

```bash
npm run prepare:config   # optional, for local gateway
npm run dev
npx @evenrealities/evenhub-simulator http://localhost:5173
```

`evenhub pack`/`qr`은 별도 CLI다: `npm i -g @evenrealities/evenhub-cli` (`eh` 별칭 동일).

⚠️ **시뮬레이터는 `linux-arm64` 빌드가 없다** (`darwin-arm64` · `darwin-x64` · `linux-x64` · `win32-x64`만). 게이트웨이 호스트가 ARM이라 거기서는 실행되지 않는다.

### 시뮬레이터 스모크 (행동 검증)

단위 테스트는 순수 정책(백오프·서명·인덱스 가드)만 본다. **HUD가 실제로 얌전한지**는 공식 시뮬레이터의 자동화 API(`--automation-port`, v0.7.0+)로만 관측된다.

```bash
npm run smoke     # x86_64에서만 실제 실행, ARM에서는 SKIP(exit 0)
```

하네스가 고정하는 것 — 부팅 렌더 · 콘솔 무예외 · 탭 반응, 그리고 **코드로만 고쳤던 두 가지**:

| 주장 | 관측 방법 |
|---|---|
| 배경 갱신이 화면을 흔들지 않는다 | 스텁이 고정 페이로드 → 한 주기 뒤 프레임버퍼 **바이트 동일** |
| 종료하면 폴링도 멈춘다 | 더블탭 후 한 주기 동안 스텁 요청 수 **불변** |

게이트웨이는 `test/stub-gateway.mjs`(고정 응답 + 요청 카운터)로 세우므로 실 데이터·네트워크가 필요 없다. 실패 시 스크린샷·콘솔이 `smoke-artifacts/`에 남고 CI가 아티팩트로 올린다.

### In-glass controls

| 입력 | 동작 |
|---|---|
| 탭 | Glance 새로고침 (`?fresh=1`) |
| 스와이프 ↓ | `/api/even/status` (브리지/챗 준비 상태) |
| 스와이프 ↑ | 이전 페이지 |
| 더블탭 | 종료 확인 |

QR 시드의 `?seed=` 쿼리는 저장 직후 URL에서 제거됩니다 (토큰이 주소창에 남지 않게).

### 배경 갱신 규칙

시야에 떠 있는 디스플레이라 **배경 폴링은 보이지 않아야 한다**는 것이 이 루프의 설계 원칙이다.

| 규칙 | 이유 |
|---|---|
| 배경 갱신은 로딩 문구를 쓰지 않는다 | 45초마다 시야가 `불러오는 중…`으로 덮이던 문제 |
| 내용이 같으면 다시 그리지 않는다 | 컨테이너 재구축이 곧 깜빡임 (`payloadSignature`로 비교, 상대시각 스탬프는 비교 제외) |
| 배경 실패는 화면에 띄우지 않는다 | 통신 범위를 벗어나면 매 주기 오류가 번쩍이던 문제 |
| 연속 실패 시 45초 → 최대 12분으로 백오프 | 도달 불가한 게이트웨이를 무한 재시도하지 않게 (탭하면 즉시 리셋) |
| 요청은 10초에 중단 | 타임아웃이 없으면 `busy`가 영원히 걸려 탭·스와이프가 전부 죽는다 |
| 더블탭 종료 시 폴링도 함께 멈춘다 | 닫힌 앱이 계속 네트워크를 치던 문제 |

읽는 중인 알림이 배경 갱신 사이에 사라지면 목록으로 바꿔치지 않고 `이 알림은 처리됐습니다`를 보여준다.

### 검증

```bash
npm run typecheck && npm test && npm run build
```

나이틀리 드리프트 감시(`.github/workflows/nightly-drift.yml`)의 `even-g2` 잡이 같은 3단계를 돌린다.

## Notification HUD (P0)

Gateway proactive pushes are normalized to one-line HUD grammar before SSE/FCM:

- title ≤ ~18 runes (e.g. `Deneb · 업무`)
- body single line ≤ ~100 runes, markdown/URLs stripped

### Even 알림 필터 + 측정

1. Phone Even app → Notifications: allow Deneb / FCM channel only (or the app that surfaces Deneb pushes).
2. For one workday, tally **폰을 꺼보지 않고** 안경 HUD만으로 처리한 알림 횟수 (`HUD_OK`) vs 폰을 연 횟수 (`PHONE_OPEN`).
3. Success signal: `HUD_OK / (HUD_OK + PHONE_OPEN)` rising across a few days.
4. Optional diary line: `wiki log` — `G2 HUD: OK=N phone=M`.

## Open questions — need the actual glasses

리스트 컨테이너의 동작 두 가지가 시뮬레이터만으로는 결론이 안 난다. 시뮬레이터 README 가 **바로 이 영역**의 충실도를 명시적으로 부인하기 때문이다:

> **List Behavior** — List scrolling behavior, especially focused-item positioning on screen, can vary. This happens because the simulator re-implements drawing logic instead of sharing embedded source code directly.

그래서 스모크는 둘 다 **단정하지 않고 관측만** 한다 (`smoke-artifacts/observations.json`).

시뮬레이터에서 확인된 사실: **스와이프는 리스트에 도달한다**. 첫 `down` 이 선택 테두리를 다음 항목으로 옮긴다(스모크가 이건 단정으로 검사한다). 즉 입력이 유실되는 게 아니라, 호스트가 스크롤을 **자기 선택 이동에만 쓰고** 앱에는 아무것도 넘기지 않는다.

### 1. 목록 페이지에서 다음 페이지로 넘어갈 수 있는가? (`listPaging`)

선택이 마지막 항목에 닿은 뒤 `down` 을 더 눌러도 아무 일도 없다 — `SCROLL_BOTTOM` 이 앱에 오지 않는다. **실기기에서도 그렇다면 실제 결함이다**: 목록에서 탭은 상세를 열고 더블탭은 앱을 종료하므로, 알림이 있는 동안 일정/할 일 페이지가 통째로 닿지 않는다.

### 2. 탭이 '선택된' 항목을 여는가? (`listSelectionHonoured`)

이쪽이 더 위험하다. 지금까지 관측된 `listEvent` 에는 `currentSelectItemIndex` 가 **한 번도 실려오지 않았다** — 그래서 `resolveSelectionIndex` 는 0번 항목으로 폴백한다. 선택을 2번 항목으로 옮긴 뒤 탭하면 **1번 항목이 열린다**는 뜻이다. 착용자가 고른 것과 다른 알림을 읽게 되는, 못 넘어가는 것보다 나쁜 실패다.

**실기기 확인 절차**: 알림 2건 이상인 상태에서 ① 아래 스와이프로 두 번째 알림 선택 → ② 탭 → 열린 상세가 **두 번째** 알림인지 확인. ③ 다시 목록으로 나와 아래 스와이프를 계속했을 때 일정/할 일 페이지로 넘어가는지 확인.

**둘 다 실패라면** 수리 방향은 알림 페이지에서 리스트 컨테이너를 걷어내는 것이다. 선택 인덱스가 안 온다면 리스트는 선택 기능을 주지도 못하면서(항상 0번) 페이지 이동만 빼앗는 셈이라, 텍스트 페이지 대비 순손실이다.

## Design constraints

- Canvas 576×288, monochrome green text containers
- Glance is structured data (no agent turn); Custom AI still uses chat sync
- Secrets: prefer QR seed; never commit `runtime-config.json` or `.ehpk`
