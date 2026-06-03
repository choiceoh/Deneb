# Deneb

**Chief-of-Staff–style single AI agent for NVIDIA DGX Spark (비서실장형 단일 에이전트).** One persona that performs **업무분석** (deep context — mail, projects, people, deals) and **업무비서** (proactive ops — calendar, meeting prep, capture) in lockstep — same head, two hands. Native client → Go gateway server. Single-user, single-machine deployment. Korean-first. General assistant capabilities are preserved.

- **Go gateway** (`gateway-go/`): HTTP/WS server, RPC dispatch, session management, chat/LLM pipeline, 150+ tool integrations, native-client `miniapp.*` RPC surface.

---

# Repository Guidelines

- Repo: https://github.com/deneb/deneb
- In chat replies, file references must be repo-root relative only (example: `gateway-go/internal/runtime/server/server.go:80`); never absolute paths or `~/...`.
- Do not edit files covered by security-focused `CODEOWNERS` rules unless a listed owner explicitly asked for the change or is already reviewing it with you. Treat those paths as restricted surfaces, not drive-by cleanup.

---

## Context Engineering Policy

> **필요한 규칙만, 필요한 시점에, 필요한 만큼.**

이 프로젝트는 **조건부 규칙 로딩** 원칙을 따릅니다:

- **CLAUDE.md (이 파일)**: 모든 작업에서 항상 필요한 핵심 규칙만 유지합니다. 새 규칙 추가 시 여기에 넣기 전에 "정말 모든 작업에 필요한가?"를 먼저 판단하세요.
- **`.claude/rules/*.md`**: 주제별/모듈별 조건부 규칙 파일. 각 파일의 frontmatter에 `description`과 `globs` 패턴을 명시하여, 해당 파일이 수정될 때만 자동 로딩됩니다.
- 규칙을 추가/수정할 때는 반드시 이 분류 체계를 따르세요. CLAUDE.md가 비대해지면 컨텍스트 품질이 저하됩니다.

### Rules Index

| File | Scope | Globs |
|---|---|---|
| `architecture.md` | 프로젝트 구조/모듈맵 | `cmd/**`, `internal/**`, `pkg/**` |
| `go-gateway.md` | Go 게이트웨이 구조 | `gateway-go/**` |
| `docs.md` | 문서 작성 표준 | `docs/**` |
| `generated-code.md` | 생성 코드 수정 금지 | 생성 파일 직접 지정 |
| `testing.md` | 테스트 가이드라인 | `**/*_test.go` |
| `release-and-deploy.md` | 릴리스/배포 워크플로우 | `scripts/release*`, `.github/workflows/release*` |
| `git-pr.md` | Git/PR 상세 가이드 | `.github/**` |
| `build-status.md` | CI 빌드 상태 확인 | `.github/workflows/**`, `scripts/build-status` |
| `collaboration.md` | 협업/보안/멀티에이전트 | `**` |
| `hub-wiring.md` | GatewayHub 배선 규칙 | `gateway-go/internal/runtime/server/method_registry.go`, `gateway-go/internal/runtime/rpc/rpcutil/gateway_hub.go` |
| `live-testing.md` | 라이브 테스트 필수 절차 | `gateway-go/**/*.go` |
| `optimization.md` | 반복 최적화 전략 (오토리서치 방법론) | `gateway-go/**/*.go` |
| `concurrency.md` | 뮤텍스/채널/goroutine 규칙 (데드락 방지) | `gateway-go/**/*.go` |
| `logging.md` | slog 레벨 가이드 (사용자 무응답 Error 원칙) | `gateway-go/**/*.go` |
| `prompt-cache.md` | 프롬프트 캐시 불가침 원칙, 3계층 구조, cache-aware 슬래시 | `gateway-go/internal/pipeline/chat/prompt/**`, `gateway-go/internal/pipeline/chat/slash_commands.go` |
| `sidecar-models.md` | GPU 부가 모델 운영 현황 (PaddleOCR-VL OCR·추출·임베딩) | `gateway-go/internal/pipeline/chat/tools/paddleocr.go`, `gateway-go/internal/ai/modelrole/**`, `gateway-go/internal/pipeline/pilot/**` |
| `native-design-system.md` | 네이티브 클라 디자인 경계 (컨트롤=Material, 외형=Deneb 타이포) | `client-android/app/composeApp/src/**/*.kt` |

---

## Agent Quick-Start

> Run these when starting a new coding session.

1. **Check environment:** `./scripts/check-dev-env.sh`
2. **Build Go gateway:** `make go`
3. **Run tests:** `make test`
4. **Fast iteration:** `make go-dev` (auto-restart)
5. **Live test:** `scripts/dev/live-test.sh restart && scripts/dev/live-test.sh smoke` (코드 변경 후 실제 동작 검증 필수)

**Module guides:** Each module (`gateway-go/`, `skills/`) has its own `CLAUDE.md` with targeted build/test/contribution guidance.

---

## Project Philosophy

> **All AI agents MUST read and internalize this section before making any changes.**

### Agent Persona (비서실장형 단일 에이전트)

> 분석가와 비서를 분리된 두 인격으로 두지 않는다. 청와대 비서실장처럼 **한 머리가 두 역할을 동시에 수행**한다.

- **업무분석가 모드 (반응형·깊이)**: 메일/문서/관계/자금 컨텍스트 합성, 리스크 플래그, 의사결정 근거 제공.
- **업무비서 모드 (능동형·간결)**: 일정·미팅 준비·캡처(녹음/OCR/카톡 페이스트)·임박 알림.
- **통합 원칙**: "왜 지금 중요한가(분석)"와 "언제까지 처리해야 하나(비서)"가 한 응답에서 같이 나와야 의사결정 보조가 된다.
- **UI 분리 금지**: 미니앱·텔레그램 모두 "분석 탭 / 비서 탭"으로 가르지 말 것. 데이터 레이어·화면·페르소나 모두 통합 유지. (이는 *페르소나* 분리 금지이지 기기별 반응형 레이아웃 금지가 아니다 — 미니앱의 PC/모바일 레이아웃 차이는 허용·권장.)
- **개입 기준**: 능동적이되 침해적이지 않게. 필요한 순간에만 끼어든다 (over-notification 금지).

### Deployment Environment

- **Single operator, single user.** No multi-tenant, multi-user, or team deployment. Ignore user isolation, permission separation, multi-user auth.
- **Hardware:** NVIDIA DGX Spark (local server). All services run on this single machine.
- **Primary I/O surface:** the native client (`client-android/`, a Kai-fork KMP app) — the sole user surface since the Telegram bot was retired (PR #1922). It runs on Android (Samsung Galaxy S25, the daily driver), iOS, and desktop (Mac) from one codebase, and talks to the gateway over the `miniapp.*` RPC surface with an `X-Deneb-Client-Token`. Optimize this path first.

### Design Principles

- **High completeness and cohesion.** Every feature must be fully finished and tightly integrated.
- **Opinionated defaults over user configuration.** Apple-like philosophy: fewer moving parts, not more options.
- **Narrow scope, deep quality.** Fewer things well > more things shallowly.
- **Depth over breadth.** Optimize the narrow supported surface (native client + DGX Spark + single user). "Narrow" means one user + one backend — not one device class: the native client (`client-android/`) spans phone touch and desktop (mouse/keyboard) from one codebase.

### AI Agent Guidelines

- All development is **vibe coding** — leave sufficient context and comments for the next AI session.
- Break complex logic into small, well-named functions.
- Prefer simple sequential processing over concurrency/race-condition handling.

### Native Client Optimization (Android-first)

- Optimize for the native client's `miniapp.*` RPC surface and rich rendering (Markdown, native lists/cards) — no Telegram-style hard 4096-char cap or MarkdownV2 escaping.
- Design system: controls = Material, presentation = Deneb typography (see `.claude/rules/native-design-system.md`).
- **Adapt layout to the screen, never split the persona:** rendering may differ by surface (native client phone vs. desktop), but that is orthogonal to the "UI 분리 금지" persona rule — it forbids splitting 분석/비서 *personas* into tabs, not adapting layout to screen size.

### Korean Language First

- Default to Korean for UI text, responses, and user-facing messages. No i18n framework needed.

### DGX Spark

- Local GPU inference available — minimize external API calls, leverage aggressive caching/preloading.
- Deployment is simply `git pull` + restart.

---

## Code Style Essentials

- Language: Go (`gateway-go/`).
- Go: `gofmt`/`go vet`.
- Naming: **Deneb** for product/app/docs headings; `deneb` for CLI/package/binary/paths/config.
- American English in code, comments, docs, UI strings.
- Keep files under ~700 LOC; split/refactor when it improves clarity.
- Add brief comments for tricky or non-obvious logic only.

---

## Build Hard Gates

- Before any commit touching `gateway-go/`: run `make check` and it MUST pass.
- Do not commit or push with failing build or test checks.
- Toolchain: Go (1.24+).

---

## Live Testing Hard Gate

> 단위 테스트 통과 ≠ 제품 품질. 코드 변경 후 가능한 한 라이브 검증.

**현재 enforced 게이트는 `make check`** (build + `-race` 단위 테스트). 커밋 전 반드시 통과.

```bash
scripts/dev/live-test.sh restart    # 빌드 + dev 게이트웨이 재시작
scripts/dev/live-test.sh smoke      # Health + Ready 확인 (HTTP /health — 동작함)
scripts/dev/live-test.sh logs-errors  # 숨은 에러 확인
scripts/dev/live-test.sh stop       # 정리
```

> **⚠️ 채팅 기반 라이브 테스트(`chat`/`quality`/`chat-check`)는 현재 동작하지 않는다.**
> 이 경로는 목 텔레그램 서버(`scripts/mock_telegram_server.py`)에 메시지를 주입하고
> 게이트웨이의 Telegram 플러그인이 `TELEGRAM_API_BASE`로 폴링하는 구조였다.
> PR #1922로 Telegram 플러그인이 제거되어 게이트웨이는 더 이상 `TELEGRAM_API_BASE`를
> 읽지 않으므로 주입된 메시지가 파이프라인에 도달하지 않는다. 네이티브 클라이언트
> 주입 경로(`miniapp.*` RPC)로의 재작성이 필요하다 (후속 과제). 그 전까지는
> `make check` + `smoke` + `logs-errors`로 검증한다.

- **로그에서 에러/경고 없는 것까지 확인**해야 진짜 완료.
- 포트: dev=18790, iterate=18791, prod=18789 (프로덕션 영향 없음).
- 상세 절차/명령어: `.claude/rules/live-testing.md` 참조.

---

## Git Commit Format (REQUIRED)

All commits MUST use Conventional Commit format:

**Correct:** `feat(chat): add send_file tool` / `fix(memory): resolve deadlock`
**Incorrect:** `chat: add send_file tool` ❌ (module-only prefix dropped from changelogs)

**Allowed types:** feat, fix, perf, refactor, docs, test, chore, ci, build
**Allowed scopes:** any module name (chat, pilot, memory, vega, aurora, miniapp, etc.)
