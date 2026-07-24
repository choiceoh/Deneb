# Even Realities G2 × Deneb 연동 조사

> **상태**: research snapshot (2026-07-15) · **P1 bridge landed** (`gateway-go/internal/runtime/evenapi`, `even-g2/` Glance scaffold) · 2026-07-24  

> **범위**: Even Realities G2 하드웨어·Even Hub SDK·Custom AI / Add Agent·커뮤니티 브리지 패턴 ↔ Deneb 게이트웨이·네이티브 클라·회의/번역 표면  
> **방법**: Even Hub 공식 문서(Overview / Architecture / Device APIs / Display / FAQ / Packaging, 2026-06~07), 제품·지원 Specs, OpenClaw·Hermes/Juiz 커뮤니티 연동 글, BLE reverse-engineering 레포, Deneb `gateway-go` OpenAI/miniapp 표면 교차 확인  
> **주의**: `docs/research/` 스냅샷 — 현행 SoT는 루트 `CLAUDE.md` + `docs/agent-rules/*`. 마케팅 수치와 SDK 캔버스가 어긋날 때는 **SDK를 우선**.

---

## 0. TL;DR

Even G2는 Deneb의 **두 번째 풀 클라**가 아니라, **시선 HUD + 현장 마이크 입구**다.

| 결론 | 내용 |
|---|---|
| **최고 ROI** | Even 앱 **Custom AI / Add Agent** → 얇은 브리지 → Deneb 에이전트 (OpenClaw/Hermes가 이미 검증한 계약) |
| **즉시 체감** | Deneb 푸시·알림을 G2 알림 HUD로 (코드 최소) |
| **중기** | Even Hub 플러그인 “Deneb Glance” — 일정/긴급 메일/오늘 할 일 2~4줄 |
| **흡수 가치** | Conversate 녹취·요약 → Deneb 위키/사람/할일 (캡처는 G2, 기억은 Deneb) |
| **비권장** | 풀 챗·deneb-ui 카드·표 이식, 직접 BLE 스택, 안경 비전 AI (카메라 없음) |
| **Deneb 갭** | 메인 게이트웨이의 주 표면은 `miniapp.*` RPC. Agent-facing OpenAI chat completions는 **없음**(wormhole의 `/v1/chat/completions`는 LLM 프록시). 따라서 **브리지가 필수**. |

한 줄: *이미 똑똑한 Deneb 출력을 손이 없을 때 보이게* 하는 장치 — 제품을 얇게 잡을수록 연동 가치가 커진다.

---

## 1. 하드웨어 & 제품 표면

### 1.1 Specs (지원 센터 / Hub Overview)

| 항목 | 값 | 연동 함의 |
|---|---|---|
| Display | Micro-LED, green, binocular waveguide | 고대비 텍스트 HUD |
| Canvas (SDK) | **576×288** per eye, 4-bit greyscale (16 levels) | UI 설계 기준 |
| Marketing resolution | 640×350 (support Specs) | 문서 불일치 — **SDK 우선** |
| FoV / brightness | ~27.5°, up to ~1200 nits | 실외 가독성 양호 |
| Audio in | 4-mic array → single stream PCM **16 kHz**, s16le, mono | 플러그인 캡처·ASR 가능 |
| Audio out | **없음** | 답변은 글자만; TTS는 폰/이어폰 |
| Camera | **없음** (privacy-by-design) | 비전/현장 사진 이해 불가; 폰 카메라 API만 |
| Input | Temple press / double / swipe; optional **R1** ring (same gestures) | 메뉴·스크롤 수준 |
| IMU | 스트리밍 가능 (pace codes) | 착용/모션 보조 |
| Connectivity | BLE 5.2/5.4 to phone | 안경에 앱 로직 없음 |
| Battery | Glasses ~192 mAh, “regular use ~2 days”; case ~7 recharges | 상시 착용 현실적 |
| Durability | IP65 | 일상·출장 착용 가능 |
| Fit | G2 A / G2 B 프레임, RX lens option | 일상 안경 대체 가능 |

### 1.2 소비자 내장 앱 (리뷰·제품 페이지)

기본 메뉴 예: Notifications, Conversate, Teleprompt, Translate, Navigate, Even AI, Dashboard, Silent Mode.

| 기능 | 역할 | Deneb와의 관계 |
|---|---|---|
| **Conversate** | 현장 녹취·화자/요약·앱 내 transcript | 캡처는 G2; **정착(위키·사람·할일)은 Deneb** |
| **Translate** | 실시간 자막, 35+ 언어 | Deneb 인앱 DeepL 브라우저는 **화면/문서** — 보완 |
| **Navigate** | 도보/자전거 턴바이턴 | 대체 대상 아님 |
| **Teleprompt / QuickList** | 프롬프터·음성 할일 | 대체 대상 아님 |
| **Even AI** | 일반 Q&A | Custom AI로 **Deneb 교체**가 핵심 연동 |
| **Notifications / Dashboard** | 필터 알림·위젯 | Deneb 선제 푸시의 HUD 채널 |

---

## 2. 개발 플랫폼 (Even Hub)

### 2.1 아키텍처

```
Even Hub Cloud (배포/호스팅)
        │ HTTPS
Phone: Even Realities App (Flutter) + WebView plugin
        │ BLE
Even G2 (display + input + mic stream)
```

- 플러그인 로직은 **폰 WebView** (Android Chromium / iOS WKWebView).
- 안경은 컨테이너 합성·제스처·오디오 스트림만; **임의 픽셀/HTML 렌더 없음**.
- 배포: Hub catalog `.ehpk` / private build / QR sideload / **PWA URL**(포털 우회 가능).

### 2.2 SDK 표면 (`@evenrealities/even_hub_sdk`)

**Display**

- Containers: Text / List / Image (페이지당 image ≤4, 기타 ≤8).
- Exactly one `isEventCapture: 1`.
- Text: 고정 폰트, 정렬/굵기/배경 없음; `\n` 줄바꿈; overflow 시 capture 컨테이너면 펌웨어 스크롤.
- Char limits: startup/rebuild 1,000; `textContainerUpgrade` 2,000.
- 실용 한도: 풀스크린 텍스트 ≈ **400–500자** (커뮤니티는 G2 답변 **~400자 truncate** 권장).
- Image: ≤288×144, greyscale; startup 중 전송 금지 → 이후 `updateImageRawData` (SDK 0.0.12+ LZ4).
- List: ≤20 items × ≤64 chars; in-place update 없음 → rebuild.
- BLE 대역 ≈ 10–30 KB/s → “애니메이션” 비현실적.
- Emoji 불가 → 기하/박스 문자.

**Lifecycle**

- `createStartUpPageContainer` 1회 → `textContainerUpgrade` / `rebuildPageContainer` → `shutDownPageContainer(1)` (루트는 시스템 종료 확인 필수, 리뷰 거부 사유).

**Device APIs**

| API | 메모 |
|---|---|
| Touch / R1 | CLICK / DOUBLE / SCROLL_TOP / SCROLL_BOTTOM (`CLICK_EVENT` 0 → SDK가 `undefined`로 normalize하는 경우 있음) |
| `audioControl` | Glasses 또는 Phone mic; PCM via `audioEvent` |
| Location | 폰 GPS one-shot / continuous |
| Photos | 폰 album picker / camera capture → base64 |
| IMU | `imuControl` + pace codes |
| Device / user info | battery, wearing, charging, in-case; uid/name… |
| Local storage | WebView 영속 (uninstall 시 삭제); 앱 간 공유 불가 |

**명시적 비노출 (FAQ)**

- 직접 BLE, 임의 픽셀, 오디오 출력, 폰트/정렬/배경, 플러그인 push 수신, 백그라운드 네트워크.
- WebView 백그라운드 시 중단 → WebSocket drop; in-flight fetch stall.
- 네트워크: `app.json` **whitelist + 서버 CORS** 둘 다 필요.
- Permissions: `network`, `location`, `g2-microphone`, `phone-microphone`, `album`, `camera`.
- `supported_languages` includes **`ko`**.
- Secrets must not ship inside `.ehpk`.
- Enterprise/ deeper HW: `hello@evenrealities.com` (2B/2G).

### 2.3 로드맵 표면 (미개방)

공식 Overview: **plugins live today**; coming — dashboard widgets, dashboard layouts, **AI skills**.

### 2.4 툴링

- CLI `evenhub` / `eh`: init, qr, pack.
- Simulator + headless HTTP API (CI 가능).
- Claude Code plugin catalog: `everything-evenhub` (quickstart, glasses-ui, device-features, …).

---

## 3. Custom AI / Add Agent (에이전트 연동 계약)

공식 “Agent HTTP API” 문서는 빈약하고, 커뮤니티 reverse probe가 실질 SoT다.

### 3.1 관측된 요청 (OpenClaw 브리지, webhook.site)

```http
POST / HTTP/1.1
Authorization: Bearer <token>
User-Agent: Dart/3.8 (dart:io)
x-openclaw-agent-id: main
Content-Type: application/json

{
  "model": "openclaw",
  "messages": [
    { "role": "user", "content": "<on-device STT text>" }
  ]
}
```

포인트:

1. **음성→텍스트는 Even 앱 측** — 서버로 오는 것은 transcript.
2. 바디는 chat-completions 형태 (`messages[]`).
3. 일부 빌드는 **루트 POST**, 다른 설정 UI는 **`/v1/chat/completions`** (Hermes/Juiz + Tailscale 사례).
4. Even Hub가 OpenClaw를 1st-class로 인지 (`model: "openclaw"`, `x-openclaw-agent-id`).
5. 실측 타임아웃 대략 **~10–30초** (브리지들은 10–22s 데드라인 사용).
6. 동일 POST 재시도 가능 → 브리지 **dedupe** 권장.

### 3.2 기대 응답

```json
{
  "id": "chatcmpl-…",
  "object": "chat.completion",
  "created": 1770000000,
  "model": "…",
  "choices": [
    {
      "index": 0,
      "message": { "role": "assistant", "content": "짧은 평문" },
      "finish_reason": "stop"
    }
  ]
}
```

G2 표시용으로 URL·코드펜스·마크다운을 걷어내고 **~400자**로 자르는 패턴이 반복된다.

### 3.3 커뮤니티 라우팅 패턴 (OpenClaw Worker)

| 경로 | 동작 |
|---|---|
| Short | Gateway 동기 회신 → 안경 |
| Long | 즉시 ack → 백그라운드 실행 → 폰/Telegram 등 리치 채널 |
| Image | ack → 생성물 폰 채널 (안경은 사진 비표시) |

Hermes/Juiz: Tailscale IP의 사설 `…/v1/chat/completions` + request_id / pending Promise + tmux proxy.

참고:

- [OpenClaw × G2 bridge write-up](https://blog.juchunko.com/en/even-realities-g2-openclaw-bridge/)
- [openclaw-even-g2-bridge-skill](https://github.com/dAAAb/openclaw-even-g2-bridge-skill)
- [Hermes/Juiz Tailscale bridge (note.com)](https://note.com/yukyu30/n/n0089674e6453)

---

## 4. Deneb 측 ground truth (조사 시점)

| 표면 | 상태 | G2 함의 |
|---|---|---|
| `miniapp.*` RPC + `X-Deneb-Client-Token` | 주 클라 계약 (Android) | 플러그인/브리지가 호출할 **정본** |
| Chat sync / tools / wiki / mail / calendar | 비서 본체 | 짧은 질의·Glance 데이터 소스 |
| Wormhole `POST /v1/chat/completions` | **LLM 라우터/프록시** | Agent ingress로 오해하면 안 됨 |
| Agent-facing OpenAI chat completions | **없음** (조사 시점) | Custom AI → **브리지 필수** |
| ASR 사이드카(MOSS-Transcribe-Diarize) | 회의/보이스 메모 | 플러그인 PCM 경로 후보 |
| 인앱 DeepL 번역 브라우저 | 화면/문서 | G2 Translate와 역할 분리 |
| FCM / SSE 푸시 | 선제 알림 | 알림 미러 0차 연동 |
| Android Auto / assist surfaces | 존재 | G2와 겹치지 않는 별 레인 |

보안: 브리지·플러그인에 장기 시크릿을 넣지 말 것. Tailscale(또는 동등 사설망) + Bearer, gateway CORS/whitelist 정합.

---

## 5. 연동 아키텍처 옵션

### Path A — Custom AI 브리지 (권장 1순위)

```
G2 mic → Even STT → Even App Custom AI
        → Bridge (OpenAI-shaped I/O, timeout, cleanForG2, short/long split)
        → Deneb (miniapp.chat.send / sync chat, dedicated session e.g. glasses:*)
        → short text → glasses
           long result → Android / other rich surface
```

**Pros**: 내장 “Hey Even” UX, STT 재구현 불필요, 커뮤니티 검증됨, 구현량 작음.  
**Cons**: 타임아웃·평문 제약; Even 앱 버전별 URL 차이; 툴 루프 긴 턴은 ack 패턴 필수.

### Path B — Even Hub 플러그인 “Deneb Glance”

```
Plugin WebView → HTTPS (whitelist + CORS) → Deneb miniapp RPC
              → textContainerUpgrade (오늘 요약 / 다음 일정 / 긴급 1건)
```

**Pros**: 레이아웃·제스처 통제, 폴링/수동 새로고침.  
**Cons**: 포그라운드 전용; 백그라운드 푸시 불가; Hub 리뷰/패키징 또는 PWA 운영.

### Path C — 알림 미러 (0차)

Deneb 알림 제목/본문을 **한 줄 HUD 문법**으로 통일 → Even 알림 필터.

**Pros**: 코드 최소, 도착 즉시 ROI.  
**Cons**: 상호작용·질의 없음; OS 알림 정책 의존.

### Path D — 회의 흡수

1. Conversate export → Deneb wiki/meeting (반자동부터).  
2. 또는 플러그인 `audioControl(Glasses)` → PCM → Deneb ASR → 위키 (Conversate와 경쟁·배터리·복잡도↑).

**권장**: 1을 먼저; 2는 Conversate API/export가 막힐 때만.

### Path E — 직접 BLE (비권장)

커뮤니티: `i-soxi/even-g2-protocol` (auth handshake, teleprompter partial, dual channel 0x5401/0x6402, …).  
지원 밖·깨지기 쉬움·Even 앱과 충돌. Deneb 제품 경로로 채택하지 않는다.

---

## 6. 역할 분담 매트릭스

| 유스케이스 | G2 네이티브 | Deneb | 권장 |
|---|---|---|---|
| 다음 일정 / 긴급 메일 glance | 알림·대시보드 | 생성·우선순위 | C → B |
| “다음 미팅 뭐지?” 음성 | Even AI / Custom AI | 도구+기억 | **A** |
| 긴 분석·코딩·카드 UI | 불가에 가까움 | Android / Andromeda | A long-route |
| 현장 대화 자막 | Translate | — | 네이티브 |
| 웹/문서 DeepL | — | 인앱 브라우저 | Deneb |
| 미팅 기억·할일·사람 | Conversate 로컬 | 위키·지식 | **D absorb** |
| 현장 비전 | 불가 | 폰 카메라 툴 | 기대하지 말 것 |

---

## 7. 제안 로드맵

| Phase | 산출 | 성공 기준 | 의존 |
|---|---|---|---|
| **P0** 도착 직후 | 알림 필터 + Deneb 푸시 한 줄 포맷 실험 | 하루 착용 중 “폰 안 꺼도 되는” 횟수 | 코드 거의 없음 |
| **P1** Custom AI 브리지 | OpenAI-shaped endpoint → Deneb sync chat; Tailscale; `cleanForG2`; short/long | 짧은 질의 p50 < timeout; 긴 일은 ack+폰 회신 | 전용 세션키·토큰 |
| **P2** Glance 플러그인 | 일정/긴급/할일 RPC → HUD; 탭 새로고침 | 출장·회의 전 30초 루틴 | CORS + whitelist |
| **P3** 회의 흡수 | Conversate → wiki/people/todos | 주간 N건 자동/반자동 정착 | export/UX |
| **P4** (옵션) | Hub AI skills 개방 시 재평가; PCM ASR는 증거 있을 때만 | — | Even 로드맵 |

**하지 않을 것 (당분간)**

- G2용 풀 Deneb 클라 / deneb-ui 이식  
- 지원 BLE 스택  
- 안경 비전 파이프라인  
- wormhole `/v1/chat/completions`를 에이전트 ingress로 재사용

---

## 8. 브리지 스케치 (P1용, 비규범)

구현 RFC가 아님 — 착수 시 별도 스펙으로 고정.

**Ingress**

- Accept `POST /` and `POST /v1/chat/completions`.
- Auth: `Authorization: Bearer <g2-bridge-token>`.
- Body: last `messages[role=user].content` as utterance.
- Optional: honor `x-openclaw-agent-id` as session/agent hint (ignore if unused).

**Egress to Deneb**

- Prefer authenticated miniapp/chat sync into a dedicated session (e.g. `glasses:main`), not LLM wormhole.
- System/style hint: short Korean plain text, no markdown/code/URLs when possible.

**Response policy**

- Deadline ~12–20s.
- If tools/run exceed deadline: return ack string; deliver full result on Android.
- `cleanForG2`: strip fences/URLs/markdown; truncate ~400 chars.
- Dedupe identical bodies within a few seconds (Even retry).

**Network**

- Prefer Tailscale (or equivalent) so the Even app reaches the bridge without public exposure.
- If Hub plugin later calls the gateway directly: CORS + `network` whitelist for the gateway origin.

---

## 9. 리스크 & 미지수

| 리스크 | 메모 |
|---|---|
| Custom AI 계약 비공식 | 앱 업데이트로 URL/헤더/타임아웃 변경 가능 |
| STT 품질·언어 | 한국어 현장 성능은 기기 도착 후 측정 |
| 듀얼 앱 마찰 | Even App + Deneb App 동시 상주·BLE/배터리 |
| Conversate export | 공개 API 불명확 → 수동/공유 경로부터 |
| 폰트 글리프 | 일부 한글/기호 누락 가능 — 실기 확인 |
| 백그라운드 | 플러그인은 포그라운드 전용; 상시 에이전트는 Custom AI·알림에 의존 |
| 리뷰/배포 | Catalog 리뷰 지연 → 개인 사용은 private build / PWA |

---

## 10. 참고 링크

### Official

- [Hub Overview](https://hub.evenrealities.com/docs/get-started/overview)
- [Architecture](https://hub.evenrealities.com/docs/get-started/architecture)
- [Device APIs](https://hub.evenrealities.com/docs/build/device-apis)
- [Display & UI](https://hub.evenrealities.com/docs/build/display)
- [Page Lifecycle](https://hub.evenrealities.com/docs/build/page-lifecycle)
- [Packaging](https://hub.evenrealities.com/docs/ship/packaging)
- [FAQ](https://hub.evenrealities.com/docs/reference/faq)
- [Claude Code tooling](https://hub.evenrealities.com/docs/AI-tooling/claude-code)
- [Product page (G2)](https://www.evenrealities.com/products/smart-glasses) (support-center Specs page returns 403 to automated checkers; physical specs also summarized in Hub Overview)
- npm package name: `@evenrealities/even_hub_sdk` (linked from [Hub docs](https://hub.evenrealities.com/docs))

### Community

- [OpenClaw × G2 bridge write-up](https://blog.juchunko.com/en/even-realities-g2-openclaw-bridge/)
- [openclaw-even-g2-bridge-skill](https://github.com/dAAAb/openclaw-even-g2-bridge-skill)
- [Hermes/Juiz Tailscale bridge](https://note.com/yukyu30/n/n0089674e6453)
- [G2 plugin starter](https://github.com/brianmatzelle/even-realities-g2-glasses)
- [even-g2-protocol (BLE RE)](https://github.com/i-soxi/even-g2-protocol)
- [even-hub-devguide](https://github.com/aleapc/even-hub-devguide) (SSE/backend patterns)

### Deneb (internal)

- Root `CLAUDE.md` — gateway + native client roles
- `docs/agent-rules/architecture.md`, `live-testing.md`, `sidecar-models.md`
- Related research: `hermes-deneb-mapping.md`, `phone-action-rfc.md`, `claw-anything-always-on-assistant.md`

---

## 11. 변경 이력

| 날짜 | 내용 |
|---|---|
| 2026-07-15 | 초판 — Hub/SDK/FAQ + Custom AI 커뮤니티 계약 + Deneb 표면 교차 + P0–P4 로드맵 |
| 2026-07-24 | P1: `evenapi` `POST /v1/chat/completions` (+ `/api/even/…`) → `glasses:main`; `DENEB_EVEN_G2_BRIDGE_TOKEN`; `even-g2/` Glance 플러그인 스캐폴드 |
