# Deneb

**Personal AI gateway for NVIDIA DGX Spark.** Telegram bot interface → Go gateway server. Single-user, single-machine deployment. Korean-first.

- **Go gateway** (`gateway-go/`): HTTP/WS server, RPC dispatch, session management, chat/LLM pipeline, 130+ tool integrations, Telegram bot plugin.
- **Protobuf schemas** (`proto/`): Cross-language type definitions (Go codegen).

---

# Repository Guidelines

- Repo: https://github.com/deneb/deneb
- In chat replies, file references must be repo-root relative only (example: `gateway-go/internal/server/server.go:80`); never absolute paths or `~/...`.
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
| `architecture.md` | 프로젝트 구조/모듈맵 | `cmd/**`, `internal/**`, `pkg/**`, `src/**` |
| `go-gateway.md` | Go 게이트웨이 구조 | `gateway-go/**` |
| `proto.md` | Protobuf 스키마 | `proto/**`, `gateway-go/pkg/protocol/gen/**` |
| `docs.md` | 문서 작성 표준 | `docs/**` |
| `generated-code.md` | 생성 코드 수정 금지 | 생성 파일 직접 지정 |
| `testing.md` | 테스트 가이드라인 | `**/*_test.go` |
| `release-and-deploy.md` | 릴리스/배포 워크플로우 | `scripts/release*`, `.github/workflows/release*` |
| `git-pr.md` | Git/PR 상세 가이드 | `.github/**` |
| `build-status.md` | CI 빌드 상태 확인 | `.github/workflows/**`, `scripts/build-status` |
| `collaboration.md` | 협업/보안/멀티에이전트 | `**` |
| `hub-wiring.md` | GatewayHub 배선 규칙 | `gateway-go/internal/server/method_registry.go`, `gateway-go/internal/rpc/rpcutil/gateway_hub.go` |
| `live-testing.md` | 라이브 테스트 필수 절차 | `gateway-go/**/*.go`, `proto/**/*.proto` |
| `optimization.md` | 반복 최적화 전략 (오토리서치 방법론) | `gateway-go/**/*.go` |

---

## Agent Quick-Start

> Run these when starting a new coding session.

1. **Check environment:** `./scripts/check-dev-env.sh`
2. **Build Go gateway:** `make go`
3. **Run tests:** `make test`
4. **Fast iteration:** `make go-dev` (auto-restart)
5. **Live test:** `scripts/dev-live-test.sh restart && scripts/dev-live-test.sh smoke` (코드 변경 후 실제 동작 검증 필수)

**Module guides:** Each module (`gateway-go/`, `proto/`, `skills/`) has its own `CLAUDE.md` with targeted build/test/contribution guidance.

---

## Project Philosophy

> **All AI agents MUST read and internalize this section before making any changes.**

### Deployment Environment

- **Single operator, single user.** No multi-tenant, multi-user, or team deployment. Ignore user isolation, permission separation, multi-user auth.
- **Hardware:** NVIDIA DGX Spark (local server). All services run on this single machine.
- **Sole I/O surface:** Telegram on Android (Samsung Galaxy S25). Optimize exclusively for this path.

### Design Principles

- **High completeness and cohesion.** Every feature must be fully finished and tightly integrated.
- **Opinionated defaults over user configuration.** Apple-like philosophy: fewer moving parts, not more options.
- **Narrow scope, deep quality.** Fewer things well > more things shallowly.
- **Depth over breadth.** Optimize the narrow supported surface (Telegram + DGX Spark + single user).

### AI Agent Guidelines

- All development is **vibe coding** — leave sufficient context and comments for the next AI session.
- Break complex logic into small, well-named functions.
- Prefer simple sequential processing over concurrency/race-condition handling.

### Telegram-Only Optimization

- Optimize for Telegram Bot API constraints: 4096-char message limit, MarkdownV2 parse mode, inline keyboards.
- Respect Telegram file size limits (50 MB for media uploads).

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

- Before any commit touching `gateway-go/` or `proto/`: run `make check` and it MUST pass.
- Do not commit or push with failing build or test checks.
- Toolchain: Go (1.24+), buf (latest), protoc, protoc-gen-go.

---

## Live Testing Hard Gate

> 단위 테스트 통과 ≠ 제품 품질. 코드 변경 후 반드시 라이브 검증.

**필수 흐름** (코드 수정 완료 후):
```bash
scripts/dev-live-test.sh restart    # 빌드 + dev 게이트웨이 재시작
scripts/dev-live-test.sh smoke      # Health + Ready + WebSocket 작동 확인
scripts/dev-live-test.sh quality    # 전체 품질 테스트 (한국어/도구/포맷/에지)
scripts/dev-live-test.sh logs-errors  # 숨은 에러 확인
scripts/dev-live-test.sh stop       # 정리
```

- **quality test 실패 시 "완료"라고 하지 마라** — 수정 → 재시작 → 재검증.
- **로그에서 에러/경고 없는 것까지 확인**해야 진짜 완료.
- 포트: dev=18790, iterate=18791, prod=18789 (프로덕션 영향 없음).
- 텔레그램 관련 수정 시 `vchat-quality` 추가 실행.
- 상세 절차/명령어: `.claude/rules/live-testing.md` 참조.

---

## Git Commit Format (REQUIRED)

All commits MUST use Conventional Commit format:

**Correct:** `feat(chat): add send_file tool` / `fix(memory): resolve deadlock`
**Incorrect:** `chat: add send_file tool` ❌ (module-only prefix dropped from changelogs)

**Allowed types:** feat, fix, perf, refactor, docs, test, chore, ci, build
**Allowed scopes:** any module name (chat, pilot, memory, vega, aurora, telegram, etc.)
