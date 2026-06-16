# Andromeda 설계 문서

> **개인 특화 AI 업무 총괄 워크스테이션** — Deneb 게이트웨이에 연동되는 macOS·Windows 데스크탑
> 클라이언트. 이 문서는 개발 착수 청사진이자 확정 결정의 단일 진실원이다.

---

## 1. 정체성 & 포지셔닝

Deneb는 비서실장형 **단일 에이전트**다. 한 머리가 업무분석(깊이)과 업무비서(능동)를 동시에 수행한다.
그 한 머리에 **두 손(클라이언트)** 이 붙는다:

| 손 | 클라이언트 | 최적화 | 상호작용 |
|---|---|---|---|
| 받는 손 | 모바일 (`client-android/`, KMP) | 정보 **수신** — 이동 중 메일·일정·피드·알림 소비 | 피드 넘기며 소비 |
| 총괄하는 손 | **Andromeda** (신규 repo) | 업무 **총괄·제작** — 데이터+문서+AI 한 화면 | 워크스테이션에서 총괄 |

**Andromeda는 데네브의 데스크탑 사령탑**이다. 메일·일정·할일·연락처·위키·문서를 밀도 높게 다루며,
그 중심에서 데네브 AI가 "지금 뭐가 중요한가 / 뭐부터 처리해야 하나"를 총괄 보조한다. 페르소나는
게이트웨이에 하나로 유지되고(CLAUDE.md "never split the persona"), Andromeda는 화면 surface만
데스크탑 업무에 맞춘다.

---

## 2. 확정 결정 로그 (2026-06-16)

| 항목 | 결정 | 근거 |
|---|---|---|
| repo 이름 | **andromeda** | 별 Deneb → 은하 Andromeda. 게이트웨이 repo 밖 신규 repo |
| 정체성 | **개인 AI 업무 총괄 워크스테이션** | 문서 도구 아님 — 메일/일정/할일/자료를 다루는 업무 사령탑 |
| 스택 | **Tauri 2 + React + admin 프레임워크(Refine 유력)** | data provider로 이식 정합 + 경량 + 이 서버 개발 가능 |
| 플랫폼 | 크로스플랫폼 (직장 Win · 집 Mac) | 두 OS 사용 → 네이티브 탈락. 경량 = Electron 회피(Tauri) |
| 연동 | `miniapp.*` RPC(data provider) + chat/stream(AI) + events(푸시) | 기존 외부 클라 계약 그대로 |
| 세션 | **`client:main` 공유** | 모바일과 같은 단일 에이전트 세션. 전용 prefix 화이트리스트 함정 회피 |
| 개발 장소 | **이 Linux DGX 서버** | Tauri Linux 빌드 + Playwright 검증 → vibe coding 루프 유지 |

### 폐기된 대안 (왜)

- **SwiftUI/WinUI/WPF (네이티브)**: 한 OS 종속 → 두 OS(직장 Win·집 Mac) 동시 불가 + 이 서버 개발 불가.
- **Electron**: 무거움(사용자 거부). Tauri가 "무게만 뺀 Electron".
- **완성 앱 포크 (AppFlowy/AFFiNE/ERP)**: 자체 데이터 모델·DB·비즈로직이 깊어 백엔드 교체가
  *재구현*이 됨. 데네브 데이터 모델(메일/위키 등)과 미스매치. → admin 프레임워크로 대체.
- **순수 노트앱 포크 (mdSilo/Noteriv)**: 마크다운 문서엔 좋지만 메일·일정 같은 구조화 데이터
  관리가 약해 "업무 총괄"엔 부족.

---

## 3. 핵심 설계 판단 — 왜 "프레임워크 빌드"가 "완성 앱 포크"를 이기는가

`client-android/`는 Kai(단순 AI 챗 앱)를 포크해 백엔드만 게이트웨이로 교체했다. 이게 쉬웠던 건
Kai의 백엔드가 "LLM 호출" 하나뿐이었기 때문이다. 그런데 업무 워크스테이션이 다룰 데이터(메일·
일정·할일·연락처·위키)는 **구조화된 다중 리소스**다. 이걸 담은 완성품(ERP·AppFlowy)은 데이터
모델이 깊어 포크 이식이 최악이다.

반대로 **admin 프레임워크의 data provider는 "백엔드를 갈아끼우라"고 만든 추상화**다:

```
Refine/react-admin  ──(data provider 인터페이스)──>  [getList, getOne, create, update, delete]
                                                              │  구현
                                                              ▼
                                              Deneb miniapp.* RPC 호출
        예) resource "mail"  → getList = miniapp.gmail.list_recent
            resource "todo"  → create  = miniapp.todo.create
            resource "memory"→ getOne  = miniapp.memory.get_page
```

data provider 하나를 데네브 RPC로 구현하면 **전 리소스가 자동으로 그리드·폼·리스트에 흐른다.**
이게 ERP·AppFlowy의 "재구현 지옥"과 정반대인, 이 방향의 결정적 우위다.

---

## 4. 아키텍처

```
┌──────────────────── Andromeda (Tauri 2 셸, Rust) ─────────────────────┐
│                          React 프론트엔드                              │
│  ┌── 좌: 네비·대시보드 ──┬─── 중앙: 작업 영역 ───┬── 우: 데네브 AI ──┐  │
│  │ 메일·일정·할일·       │ 선택 항목 상세         │ chat/stream 협업    │  │
│  │ 연락처·위키           │ 문서 에디터 / 폼 / 그리드│ tool·thinking 표시  │  │
│  │ (Refine resource 리스트)│ (Refine show/edit)    │ "오늘 뭐부터?"      │  │
│  └──────────────────────┴────────────────────────┴────────────────────┘  │
│   Refine: data provider(=RPC) + auth provider(=토큰) + 라우팅·캐싱          │
└──────────────────────────────┬─────────────────────────────────────────────┘
   miniapp.* RPC · chat/stream(SSE) · events(SSE) │ X-Deneb-Client-Token
                                ▼
                    Deneb 게이트웨이 (개인 비서실장 AI)
```

- **Tauri 셸**: Rust 코어 + 시스템 웹뷰. 경량(~수 MB), 네이티브 알림·파일·창 관리, Win/Mac/Linux.
- **React + Refine**: data provider(데네브 RPC) + auth provider(토큰). 표준 CRUD 화면은 Refine이
  생성, 워크스테이션 레이아웃(3분할·대시보드)은 커스텀.
- **AI 협업 패널**: `chat/stream` SSE 구독 → delta/tool/thinking/done 실시간 렌더. 데네브가 데이터
  컨텍스트를 보며 총괄 보조.

---

## 5. 데네브 RPC → Refine resource 매핑

| resource | 읽기(getList/getOne) | 쓰기(create/update/delete) |
|---|---|---|
| mail | `gmail.list_recent` / `gmail.get` | `gmail.archive` / `trash` / `mark_read` / `analyze` |
| calendar | `calendar.list_upcoming` / `list_range` / `get` | `calendar.create` / `update` / `delete` |
| todo | `todo.list` | `todo.create` / `update` / `set_done` / `delete` |
| memory(위키) | `memory.search` / `get_page` / `list_in_category` | `memory.create_page` / `write_page` / `move_page` / `merge` |
| people | `people.list` | (읽기 위주) |
| crons | `crons.list` / `get` | `crons.run` / `update` / `remove` |
| workfeed | `workfeed.list` | `workfeed.ack` / `action.run` |
| search | `search.all` (통합) | — |

AI 협업: `POST /api/v1/miniapp/chat/stream` (SSE, `sessionKey: "client:main"`). 캡처: `capture.image`/`audio`.

---

## 6. 연동 계약 (검증 완료)

| 표면 | 용도 |
|---|---|
| `POST /api/v1/miniapp/rpc` | 60+ RPC. envelope `{id,method,params}` → `{ok,payload}` |
| `POST /api/v1/miniapp/chat/stream` | AI 협업 (SSE: delta/tool/thinking/done/error) |
| `GET /api/v1/miniapp/events` | proactive 푸시 (SSE) |
| 인증 | `X-Deneb-Client-Token` (hex64), `~/.deneb/client_token`. Tauri는 OS 키체인/secure store에 보관 |

---

## 7. 개발 환경 (★ 이 방향의 큰 이점)

- **빌드: 이 Linux DGX 서버에서 가능.** Tauri Linux 타깃 + Rust/Node 툴체인. 실행 배포는 Win/Mac/Linux.
- **검증: AI가 이 서버에서.** 웹 프론트라 Playwright/헤드리스 브라우저로 화면·상호작용 자동 검증.
  (기존 KMP `native-app.sh`에 상응하는 Tauri/웹 하네스 신규 — 웹이라 오히려 쉬움.)
- **게이트웨이 연결:** Tailscale/LAN URL + `X-Deneb-Client-Token`.
- **AI 코딩 루프 유지:** SwiftUI와 달리 Mac 종속이 없어 이 서버의 vibe coding 그대로.

---

## 8. 단계적 계획

| Phase | 목표 | 산출물 |
|---|---|---|
| **0 — 연결 스파이크** | "붙는다" 증명 | Tauri+React 빈 셸 → 데네브 data provider(`miniapp.ping`+1개 resource) + `chat/stream` 한 줄 |
| **1 — 핵심 워크스테이션** | 업무 총괄 MVP | 3분할 레이아웃 + 메일·일정·할일 그리드(data provider) + AI 협업 패널 |
| **2 — 문서·위키 + 캡처** | 제작 강화 | 위키 에디터(memory.*), 통합 검색(search.all), capture(OCR/ASR) |
| **3 — 대시보드·딥워크** | 완성 | 커스텀 대시보드(오늘 브리핑), 멀티윈도우, 단축키, proactive 패널(events) |

---

## 9. 리스크 / 오픈 퀘스천

| 항목 | 메모 |
|---|---|
| 프레임워크 최종 (Refine vs react-admin) | Refine 유력(헤드리스·유연). 착수 시 코드로 확정 |
| data provider 형태 매핑 | 데네브 RPC가 표준 REST CRUD와 다른 부분(예: chat, 분석)은 커스텀 메서드/훅으로 |
| 문서 에디터 | 위키 마크다운 편집기 — 자체 vs 라이브러리(CodeMirror/TipTap) |
| Tauri 검증 하네스 | 이 서버용 Playwright 기반 헤드리스 검증 스크립트 신규 작성 필요 |
| 게이트웨이 측 작업 | 대부분 RPC 재사용. 필요 시 Go→TS 타입 codegen(`kotlin-models-gen` 패턴 복제) |
| 메일 작성/발송 | Phase 2+ — 게이트웨이에 `gmail.draft`/`send` RPC 추가 시 |
