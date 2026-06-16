# Andromeda

**개인 특화 AI 업무 총괄 워크스테이션** — Deneb(개인 비서실장 AI)의 **데스크탑 사령탑**.

메일·일정·할일·연락처·위키·문서를 한 화면에서 밀도 높게 다루고, 그 중심에서 **데네브 AI가
업무를 총괄 보조**한다. 기존 Deneb 모바일 클라(`client-android/`)가 이동 중 정보 **수신**에
최적화돼 있다면, Andromeda는 데스크탑에서 정보 **제작·총괄**에 최적화된 별도 repo 클라이언트다.
같은 한 머리(게이트웨이의 단일 에이전트)에 붙는 두 손 — 받는 손(모바일)과 총괄하는 손(Andromeda).

```
┌──────────────── Andromeda (Tauri 셸, 경량) ────────────────┐
│  좌: 네비·대시보드   │  중앙: 작업 영역   │  우: 데네브 AI 협업  │
│  메일·일정·할일·     │  상세·문서 에디터·  │  chat stream        │
│  연락처·위키 그리드   │  폼                │  "오늘 뭐부터?"      │
└───────────────────────────┬────────────────────────────────┘
   miniapp.* RPC · chat/stream · events │ X-Deneb-Client-Token
                            ▼
              Deneb 게이트웨이 (개인 비서실장 AI)
```

## 한눈에

| 항목 | 값 |
|---|---|
| 정체성 | 개인 AI 업무 총괄 워크스테이션 (데네브 데스크탑 사령탑) |
| 스택 | **Tauri 2** (Rust 셸) + **React** + admin 프레임워크 (**Refine** 유력) |
| 플랫폼 | 크로스플랫폼 — 직장 Windows · 집 Mac 둘 다 (경량, Electron 회피) |
| 연동 | Deneb 게이트웨이 `miniapp.*` RPC(data provider) + SSE(`X-Deneb-Client-Token`) |
| 세션 | `client:main` 공유 (모바일과 같은 단일 에이전트 세션) |
| 상태 | **설계 단계** — 상세는 [`docs/DESIGN.md`](docs/DESIGN.md) |

## 핵심 설계 판단 (왜 이 스택인가)

1. **"완성 앱 포크"가 아니라 "admin 프레임워크 빌드".** ERP·Notion형(AppFlowy) 완성품은 자체
   데이터 모델·DB가 깊어 백엔드 교체가 *재구현*이 된다. 반면 admin 프레임워크의 **data provider는
   백엔드 교체가 설계 목적** — 데네브 RPC를 한 번 구현하면 전 데이터가 그리드/폼에 흐른다.
2. **이 서버에서 AI가 개발·검증 가능.** Tauri/웹이라 Linux DGX 서버에서 빌드 + Playwright
   헤드리스 검증이 된다. SwiftUI(Mac 종속)와 달리 vibe coding 루프가 유지된다.
3. **업무 감성 + 경량.** 밀도 높은 데이터 대시보드(Windows 업무 감성) + Tauri(~수 MB, Electron
   무게 회피) + 두 OS 동시 지원.

## 개발 환경

- 빌드: **이 Linux 서버에서 가능** (Tauri Linux 타깃 + Rust/Node). 실행은 Win·Mac·Linux 전부.
- 검증: 웹 프론트라 Playwright/헤드리스 브라우저로 AI 자동 검증 가능.
- 게이트웨이 연결: Tailscale/LAN URL + `X-Deneb-Client-Token`
  (게이트웨이 호스트 `~/.deneb/client_token`).
