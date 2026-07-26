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

스텁 게이트웨이(`test/stub-gateway.mjs`)에 **런타임 모드**가 있어서, 수리가 존재하는 이유가 되는 조건을 직접 유발한다:

| 모드 | 유발하는 것 | 단정 |
|---|---|---|
| `ok` | 정상 | 배경 갱신 후 프레임버퍼 **바이트 동일** · 종료 후 요청 수 불변 |
| `alt` | 페이로드 변경 | 진짜 바뀌면 반드시 다시 그린다 (변화 감지가 닫힌 채 굳지 않았다) |
| `slow` | 응답 지연 | 폴링 중 탭이 유실되지 않는다 · 데드라인이 `busy` 를 푼다 |
| `error` | 500 | 재시도 간격이 45초 기준선을 넘어 벌어진다 · 지속 장애가 헤더에 표시됐다 지워진다 |
| `error` + 재시작 | 범위 밖 콜드 오픈 | 저장본이 뜬다 (오류 화면이 아니라) |

실 데이터·네트워크가 필요 없다. 실패 시 스크린샷·콘솔이 `smoke-artifacts/`에 남고 CI가 아티팩트로 올린다. **~15분 걸리는 건 설계** — 45초 폴링 주기와 90/180초 백오프를 실제로 기다린다.

## 폰 화면 (설정)

Even Hub 플러그인은 **폰 앱이 띄우는 WebView** 다. 그 DOM 이 곧 폰 화면이고, 글래스는 브리지 컨테이너로 따로 그린다. 이 플러그인은 오랫동안 `index.html` 의 `<body>` 가 비어 있었다 — 폰에서 열면 빈 화면이었고, **재설정 수단이 QR 시드 재발급이나 재패키징뿐**이었다. Tailscale 주소는 바뀐다.

지금은 폰 화면에 상태 + 설정 폼이 있다:

- 게이트웨이 주소 · 브리지 토큰 (QR 시드로 들어온 값이 그대로 보인다)
- **연결 테스트** — 저장 전에 *폼에 적힌 값*으로 `/api/even/status` 를 때린다. 틀린 주소를 커밋하기 전에 확인할 수 있다.
- 안경 루프 상태 미러 (연결됨 / 실패 N회 / 설정 필요)

저장하면 안경이 **재시작 없이** 새 설정으로 다시 읽는다.

두 가지가 설계상 중요하다:

- 폰 UI 는 브리지를 기다리기 **전에** 그린다. `waitForEvenAppBridge` 는 top-level await 라 그 뒤 코드는 글래스가 붙기 전까지 죽어 있다 — 연결이 고장난 바로 그 순간에 폰이 빈 화면이면 아무 쓸모가 없다.
- 텍스트 입력은 폰에만 둔다. 글래스는 제스처 4개에 키보드가 없다.

### In-glass controls

아래 [조작 (알림 페이지)](#조작-알림-페이지) 참조. QR 시드의 `?seed=` 쿼리는 저장 직후 URL에서 제거된다 (토큰이 주소창에 남지 않게).

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
| 화면 이동은 폴링에 막히지 않는다 | `busy` 가드가 45초마다 최대 10초씩 탭을 삼키던 문제 |
| 연속 2회 실패부터 헤더에 `연결 끊김` | 조용한 실패가 오래된 화면을 현재로 사칭하게 두지 않으려고 |

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
| 상세에서 더블탭 | **확인 처리** — 확인 화면 → 탭=예 / 그 외=취소 |
| 더블탭 (그 외 화면) | 종료 (폴링도 함께 멈춘다) |

### 가장 비싼 줄은 맨 윗줄이다

주변시야로 보는 디스플레이는 **위 한두 줄만 실제로 읽힌다**. 그 줄이 `Deneb · 알림` 이었다 — 착용자는 자기가 뭘 열었는지 안다. 지금은 **지금/다음 일정**(`지금 금호타이어 곡성 · 종료 20분` / `15:00 기아 광주 EPC`)이 그 자리에 온다. 아래 목록을 읽어도 알 수 없는 유일한 정보이기 때문이다. 일정이 없으면 `일정 2 · 할 일 3` 이 대신 온다.

상세 화면도 `Deneb · 상세` + 빈 줄로 10줄 중 2줄을 쓰고 있었다. 없앤 만큼 본문 예산이 120→170자로 늘었다 — 읽기가 실제로 일어나는 화면이니 그 줄들은 알림에 쓰는 게 맞다.

알림 페이지 두 번째 줄에 **시계 + 건수**가 붙는다 — 착용자가 실제로 착지하는 화면 하나가 브리핑이 되도록. 시계는 **기기 시각**으로 그린다(게이트웨이가 찍어 보내면 폴링 주기만큼 늦어 고장난 것처럼 보인다). 건수는 게이트웨이의 `counts` 필드다.

### 열 줄이 전부다

`HUD_LINES = 10` 은 추측이 아니라 **실측**이다. 알림 8건 프레임([run 30180528557](https://github.com/choiceoh/Deneb/actions/runs/30180528557) 의 `11-after-payload-change.png`)에서 11줄째 푸터가 화면 아래로 잘렸다. 그래서 알림 줄 수는 상수가 아니라 **예산**이다 — `alertSlots()` 가 제목·메타·건수줄·빈줄·푸터를 먼저 빼고 남는 만큼만 준다. 별도의 `↑/↓ N건 더` 줄은 없앴다: 제목의 `(3/8)` 이 이미 같은 말을 공짜로 한다.

같은 이유로 목록 라벨은 30자, 상세 본문은 120자에서 자른다. 줄바꿈이 조용히 일어나면 예산이 어긋나고 푸터가 화면 밖으로 밀린다.

**커서 마커는 `>` 다.** `▸` 는 이 폰트에서 **아무것도 안 그려진다** — 실제 프레임에서 확인했다. 알림이 2건 이상일 때 뭘 탭하는지 착용자가 알 수 없었다는 뜻이다.

### 조용한 실패, 조용하지 않은 거짓

배경 폴링 실패는 화면에 안 띄운다 — Tailscale 범위를 벗어날 때마다 시야에 `오류` 를 번쩍이는 건 아무 말 안 하느니만 못하다. 그런데 아무 말도 안 하면 **오래된 화면이 현재를 사칭한다**: 백오프가 12분까지 벌어지므로 착용자는 15분 전 알림을 지금 것으로 읽는다.

그래서 번쩍임 없이 헤더에 한 단어만 — 연속 **2회** 실패부터 `연결 끊김`. 1회는 폰 경유 사설망에서 흔해서 마커가 깜빡이기만 한다. 이 마커가 바뀔 때만 한 번 다시 그리고(매 실패마다가 아니라), 상세를 읽는 중이면 그리지 않는다.

**오프라인 콜드 오픈**: 마지막으로 성공한 glance 는 글래스에 저장된다(`localStorage`). 범위 밖에서 앱을 열면 오류 화면 대신 저장본을 그리고 헤더에 `오프라인 · 저장본` 을 단다 — 착용자가 가장 보고 싶은 순간이 바로 그때다. 저장본은 **첫 프레임부터** 표시되며(2회 유예는 라이브 데이터 위에서 마커가 깜빡이지 않게 하려는 것이지, 저장본은 라이브가 아니다), 렌더 불가능한 캐시는 부팅을 막지 않도록 거부된다.

### 확인 처리 (유일한 쓰기)

HUD 는 읽기 전용이었다 — 안경으로 알림을 읽어도 지우려면 폰이나 데스크톱을 열어야 해서, 같은 항목 셋이 오전 내내 시야에 다시 떴다. `POST /api/even/ack` 가 workfeed 카드를 `StatusAcked` 로 옮기고, `evenGlanceUrgent` 가 이미 그 상태를 걸러내므로 다음 폴링부터 사라진다.

제스처가 4개뿐이라 **상세에서만** 더블탭의 뜻을 바꿨다(그 외 화면에서는 그대로 종료). 되돌릴 수 없는 쓰기를 흔한 제스처에 걸었으므로 **확인 화면**을 한 단계 둔다 — 실수로 두 번 눌러도 알림이 사라지지 않는다. 취소는 읽던 상세로 정확히 되돌아간다.

게이트웨이 쪽 두 가지 주의:

- 스토어는 **ack 가 도착할 때** 찾는다. `workFeedStore` 는 `registerEarlyMethods` 에서 채워지므로, 라우트 등록 시점에 클로저가 잡으면 영영 nil 이다(실제로는 더 나빴다 — 그 시점엔 필드에 닿지도 못해 패닉했다). [[registration-phase-snapshot-trap]] 과 같은 함정.
- `alert-N` 합성 id 는 400 으로 거절한다. 원본 카드에 id 가 없을 때만 붙는 번호라 아무것도 가리키지 않는다 — 조용히 성공하면 안 된다.

### 중복 화면 제거

게이트웨이는 `home` 과 `alerts`("알림 전체") 두 페이지를 보낸다. `home` 이 텍스트로 상위 몇 건만 찍던 시절의 "전체 보기"였다. 지금은 앱이 **모든 알림을 커서+윈도우로 한 페이지에** 그리므로 둘은 같은 목록이고 제목만 다르다 — 제스처 4개짜리 기기에서 스와이프 하나가 방금 떠난 화면으로 가는 셈이었다. 알림이 있으면 `alerts` 를 건너뛴다(`skipPage`).

CI 가 이걸 못 잡은 이유도 같이 적어 둔다: **스텁이 `alerts` 페이지를 안 보내고 있었다.** 프로덕션 페이지 구성을 재현하지 않는 스텁은 이런 걸 통째로 가린다.

## 폴링은 안경이 얼굴에 있을 때만 돈다

오래 미결이던 질문 — 45초 주기가 너무 느린 건가 배터리를 태우는 건가 — 은 **추측할 게 아니었다.** 루프는 `FOREGROUND_EXIT` 에서 멈추지만 그 이벤트는 **관측된 적이 없다**(시뮬레이터가 주입 불가). 그래서 두 해석이 정반대 튜닝을 낳았다.

기기가 직접 답한다: `onDeviceStatusChanged` 가 `isWearing` · `isInCase` · `batteryLevel` · `isCharging` 을 준다.

| 상태 | 동작 |
|---|---|
| 벗음 / 충전함 | 폴링 정지 |
| 다시 착용 | **즉시 갱신** 후 재개 (지금 보고 있으니 한 주기 기다리게 하지 않는다) |
| 배터리 ≤15% & 미충전 | 주기 4배 (45초 → 3분) |
| **아무것도 보고 안 함** | **평소대로 계속 돈다** |

마지막 줄이 안전 속성이다. `isWearing` 은 와이어에서 optional 이라, 보고하지 않는 호스트에서 앱이 **영원히 갱신을 멈추면 안 된다**. `shouldPoll` 은 fail-open 이고 유닛이 그걸 고정한다.

배터리를 늦춰도 되는 이유: 능동 알림은 폰의 Notification HUD 가 따로 나른다. glance 는 정확히 느려져도 되는 쪽이다.

⚠️ 시뮬레이터는 기기 상태를 주입할 수 없다 — **판정 로직은 유닛으로 고정하되 이벤트 전달 자체는 실기기에서만 확인된다.** (`events.ts` 라이프사이클과 같은 처지.)

## 음성 — 기기 ASR 은 이미 쓰고 있다

안경/폰이 웨이크·마이크·**전사**를 다 하고, 전사된 텍스트를 Custom AI 로 보낸다 → `POST /api/even/v1/chat/completions` → `glasses:main` 세션. 프로덕션 저널에 실제 멀티턴 기록이 있다(07-24). 즉 **기기 ASR 결과는 이미 Deneb 의 것**이다.

플러그인이 못 받는 건 그 *전사문*이다 — SDK 가 앱에 주는 건 `audioControl()` 로 연 마이크의 **원시 PCM**(`audioEvent.audioPcm`, 16kHz 16bit LE)뿐이고 전사 API 는 없다. 플러그인에서 음성을 하려면 ASR 을 우리가 다시 해야 하고, 시뮬레이터는 오디오를 **실제 입력 장치**에서 받으므로(`--aid`) 헤드리스 CI 에서 검증할 수 없다. 그래서 안 만든다.

### 늦게 온 답이 안경으로 돌아온다

대신 방향을 뒤집었다. 브리지 데드라인(15초)을 넘긴 턴은 `결과는 폰 데네브에서 이어서 볼게요` 를 돌려주고, 진짜 답은 Even 이 같은 POST 를 재시도할 때만 보였다 — 아니면 **폰을 열어야** 했다. 이제 그 답이 `notice` 로 glance 응답에 실려 다음 폴링에 HUD 로 온다.

- 배경 폴링이 화면을 뺏지 않는다는 원칙의 **유일한 예외**다. 착용자가 물어본 답이기 때문이다.
- 게이트웨이는 TTL(10분) 동안 계속 제안한다 — 폴링 한 번 놓쳤다고 답이 사라지면 안 되니까. 그래서 **중복 방지는 플러그인 몫**이다(마지막으로 보여준 id 기억).
- 확인 프롬프트 위에는 안 뜬다. 파괴적 동작을 기다리는 화면을 덮으면 다음 탭이 엉뚱한 곳을 가리킨다.

## 알림 페이지는 왜 리스트 컨테이너가 아닌가

호스트의 `ListContainerProperty` 를 쓰지 않는 이유는 **측정된 것 하나**다 — 리스트는 스와이프로 자기 선택을 옮기지만 **끝에서 앱에 아무것도 보내지 않는다**([run 30179426969](https://github.com/choiceoh/Deneb/actions/runs/30179426969): `list paging: STOPS at the end of the list`). 목록에서 탭은 상세, 더블탭은 종료이므로 — 알림이 있는 동안 일정/할 일 페이지가 통째로 닿지 않았다.

**선택을 잘못 보고하지는 않는다.** `listEvent` 에 `currentSelectItemIndex` 가 안 보인다는 이유로 "고르지 않은 알림이 열린다"고 추정했으나 **틀렸다**. proto3 는 0인 스칼라를 직렬화에서 생략하므로 인덱스 부재는 곧 0번을 뜻하고, 같은 run 이 `list tap opens the SELECTED item` 을 측정했다. `resolveSelectionIndex` 의 부재→0 폴백은 이 와이어 포맷에 정확히 맞는 처리다.

텍스트 컨테이너는 스크롤(`textEvent{eventType:2}`)과 탭을 모두 앱에 전달한다. 그래서 알림 페이지는 **텍스트 한 장 + 앱이 소유하는 커서**(`listCursor`)로 그린다. 대가는 두 가지이고 둘 다 갚았다: 긴 목록 스크롤은 커서 윈도잉(`windowRange`)으로, 선택 표시는 `▸` 마커로.

부수 효과가 본질에 가깝다 — 동작이 호스트 소유에서 앱 소유로 넘어오면서 **스모크가 단정으로 검사**할 수 있게 됐다. 시뮬레이터 README 가 리스트 동작의 재현 충실도를 부인하는 영역이라, 호스트 리스트로 남겨뒀다면 영영 관측만 가능했다.

## SDK 문서에서 배운 것 (뒤늦게)

이 플러그인은 오래 `dist/index.d.ts` 를 역공학해서 만들어졌다. 패키지에 **453줄짜리 README 가 함께 온다**(`node_modules/@evenrealities/even_hub_sdk/README.md`). 타입만으로는 알 수 없고 문서에만 있는 것들:

| 사실 | 왜 중요한가 |
|---|---|
| IMU 는 `sysEvent.eventType === IMU_DATA_REPORT`(8) + `sysEvent.imuData` | 타입만 보면 채널을 틀리게 잡아 **아무것도 수신 못 한다** |
| `shutDownPageContainer(0)` = 닫기, `(1)` = 포그라운드 레이어가 결정 | 우리는 `1` 이었다 — 종료 제스처가 종료하는지 동전던지기 |
| 글래스 MIC 는 **시작 페이지를 먼저 만들어야** `true` | 순서가 틀리면 조용히 실패 |
| `isEventCapture: 1` 은 **정확히 하나**여야 한다 | |
| `zOrderIndex` 는 페이지 내 전부 넣거나 전부 빼거나, 값은 유일 | 위반 시 `createStartUpPageContainer` 가 `invalid` |
| `onLaunchSource` 는 **일찍 등록** — 로드 후 1회만 푸시 | 늦게 붙으면 영영 못 받는다 |
| 호스트가 `setLocalStorage`/`getLocalStorage` 를 제공 | 우리는 WebView `localStorage` 를 직접 쓴다 |
| `captureImageFromCamera()` · `pickImageFromAlbum()` · `getAppLocation()` | 미사용 — 현장 사진·위치가 가능하다 |

문서에도 **없는** 것: IMU 의 축 규약과 단위(`x/y/z` double 뿐). 그래서 헤드 제스처 검출기는 여전히 **실제 기록을 먼저 모아야** 쓸 수 있다.

## Design constraints

- Canvas 576×288, monochrome green text containers
- Glance is structured data (no agent turn); Custom AI still uses chat sync
- Secrets: prefer QR seed; never commit `runtime-config.json` or `.ehpk`
