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

## 조작 (알림 페이지)

| 제스처 | 동작 |
|---|---|
| ↓ / ↑ | 알림 커서 이동 → 마지막 알림에서 한 번 더 = 다음 페이지 |
| 탭 | 커서가 가리키는 알림의 상세 |
| 상세에서 ↓ / ↑ | **다음/이전 알림 상세** (목록 경유 없이 연속 열람) → 끝에서 목록으로 |
| 상세에서 탭 | 목록으로 (커서는 읽던 알림에 남는다) |
| 맨 위에서 ↑ | **새로고침** (알림 페이지는 탭이 상세라 이게 유일한 수동 갱신) |
| 더블탭 | 종료 (폴링도 함께 멈춘다) |

알림이 화면에 다 안 들어가면 커서 주변 5건만 그리고 `↑ N건 더` / `↓ N건 더` 로 알린다.

### 조용한 실패, 조용하지 않은 거짓

배경 폴링 실패는 화면에 안 띄운다 — Tailscale 범위를 벗어날 때마다 시야에 `오류` 를 번쩍이는 건 아무 말 안 하느니만 못하다. 그런데 아무 말도 안 하면 **오래된 화면이 현재를 사칭한다**: 백오프가 12분까지 벌어지므로 착용자는 15분 전 알림을 지금 것으로 읽는다.

그래서 번쩍임 없이 헤더에 한 단어만 — 연속 **2회** 실패부터 `연결 끊김`. 1회는 폰 경유 사설망에서 흔해서 마커가 깜빡이기만 한다. 이 마커가 바뀔 때만 한 번 다시 그리고(매 실패마다가 아니라), 상세를 읽는 중이면 그리지 않는다.

**오프라인 콜드 오픈**: 마지막으로 성공한 glance 는 글래스에 저장된다(`localStorage`). 범위 밖에서 앱을 열면 오류 화면 대신 저장본을 그리고 헤더에 `오프라인 · 저장본` 을 단다 — 착용자가 가장 보고 싶은 순간이 바로 그때다. 저장본은 **첫 프레임부터** 표시되며(2회 유예는 라이브 데이터 위에서 마커가 깜빡이지 않게 하려는 것이지, 저장본은 라이브가 아니다), 렌더 불가능한 캐시는 부팅을 막지 않도록 거부된다.

## 알림 페이지는 왜 리스트 컨테이너가 아닌가

호스트의 `ListContainerProperty` 를 쓰지 않는 이유는 **측정된 것 하나**다 — 리스트는 스와이프로 자기 선택을 옮기지만 **끝에서 앱에 아무것도 보내지 않는다**([run 30179426969](https://github.com/choiceoh/Deneb/actions/runs/30179426969): `list paging: STOPS at the end of the list`). 목록에서 탭은 상세, 더블탭은 종료이므로 — 알림이 있는 동안 일정/할 일 페이지가 통째로 닿지 않았다.

**선택을 잘못 보고하지는 않는다.** `listEvent` 에 `currentSelectItemIndex` 가 안 보인다는 이유로 "고르지 않은 알림이 열린다"고 추정했으나 **틀렸다**. proto3 는 0인 스칼라를 직렬화에서 생략하므로 인덱스 부재는 곧 0번을 뜻하고, 같은 run 이 `list tap opens the SELECTED item` 을 측정했다. `resolveSelectionIndex` 의 부재→0 폴백은 이 와이어 포맷에 정확히 맞는 처리다.

텍스트 컨테이너는 스크롤(`textEvent{eventType:2}`)과 탭을 모두 앱에 전달한다. 그래서 알림 페이지는 **텍스트 한 장 + 앱이 소유하는 커서**(`listCursor`)로 그린다. 대가는 두 가지이고 둘 다 갚았다: 긴 목록 스크롤은 커서 윈도잉(`windowRange`)으로, 선택 표시는 `▸` 마커로.

부수 효과가 본질에 가깝다 — 동작이 호스트 소유에서 앱 소유로 넘어오면서 **스모크가 단정으로 검사**할 수 있게 됐다. 시뮬레이터 README 가 리스트 동작의 재현 충실도를 부인하는 영역이라, 호스트 리스트로 남겨뒀다면 영영 관측만 가능했다.

## Design constraints

- Canvas 576×288, monochrome green text containers
- Glance is structured data (no agent turn); Custom AI still uses chat sync
- Secrets: prefer QR seed; never commit `runtime-config.json` or `.ehpk`
