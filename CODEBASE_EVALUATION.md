# Deneb 코드베이스 수준 평가

> 평가일: 2026-03-26 | 평가 범위: 아키텍처, 코드 품질, 테스트, 보안, CI/CD, 유지보수성
> 이전 평가(2026-03-25)는 TypeScript 코드가 존재하던 시점 기준. TS→Go/Rust 마이그레이션 완료로 전면 재작성.

Deneb는 단일 사용자(DGX Spark)를 위한 AI 게이트웨이로, Go + Rust 이원 아키텍처의 프로덕션 코드베이스다. TypeScript는 완전히 제거되었으며 `src/` 디렉토리가 더 이상 존재하지 않는다.

---

## 종합 점수

| 영역 | 점수 | 비고 |
|------|------|------|
| 아키텍처 | 9/10 | Go/Rust 이원화, FFI 경계 명확, 외부 의존성 3개 |
| 코드 품질 | 8.5/10 | 기술 부채 0건, 구조 우수, 일부 대형 파일 존재 |
| 테스트 | 8/10 | Go 54% 파일 커버리지, Rust 69% 인라인 테스트, rpc/ 취약 |
| 보안 | 9.5/10 | FFI 패닉 복구, 입력 검증, SSRF/XSS/타이밍 공격 방어 |
| CI/CD | 9/10 | 다국어 체크, 프로토 검증, 보안 스캐닝 4중 |
| 유지보수성 | 8.5/10 | AI 바이브 코딩 최적화, 65KB CLAUDE.md |
| **종합** | **8.75/10** | **마이그레이션 완료 후 안정적인 Go+Rust 코드베이스** |

---

## 1. 규모 분석

| 구성 요소 | 소스 LoC | 테스트 LoC | 파일 수 |
|-----------|----------|-----------|---------|
| Go gateway (`gateway-go/`) | 67,633 | 25,983 | 325 src + 177 test |
| Rust core (`core-rs/`) | 37,236 | (인라인) | 109 |
| Rust CLI (`cli-rs/`) | 7,944 | (인라인) | ~35 |
| Protobuf (`proto/`) | 495 | - | 6 |
| Scripts | 4,070 | - | 23 |
| **합계** | **~117,378** | **~26,000+** | **~675** |

### 의존성

- **Go 외부 의존성**: 3개 (protobuf, websocket, x/sync) — 극도로 미니멀
- **Rust workspace**: 4 crates (core, vega, ml, agent-runtime) + workspace 공통 deps (serde, prost, thiserror, regex, chrono)
- **CLI**: clap, tokio, serde, reqwest 등 표준 Rust 생태계

---

## 2. 아키텍처 (9/10)

### 강점

- **최소 의존성 철학**: Go 게이트웨이가 외부 의존성 3개만 사용 — 공급망 리스크 최소
- **FFI 설계**: Rust `staticlib` → Go CGo 인프로세스 호출, 제로 IPC 오버헤드
- **Protobuf as source of truth**: `proto/` 6개 스키마(gateway, channel, session, agent, plugin, provider)가 Go + Rust 타입 생성
- **35개 내부 모듈** (`gateway-go/internal/`): 도메인별 명확한 패키지 분리 (auth, channel, chat, cron, rpc, session, telegram 등)
- **Pure-Go 폴백**: `CGO_ENABLED=0 go build -tags no_ffi` — Rust 빌드 환경 없이도 동작
- **세션 상태 머신**: `IDLE→RUNNING→DONE/FAILED/KILLED/TIMEOUT` 명시적 전이 + 이벤트 pub/sub
- **CLI/Gateway/Core 3계층**: `cli-rs` (사용자 인터페이스) → `gateway-go` (메시지 브로커) → `core-rs` (연산 커널)

### 대형 모듈 리스크

| 모듈 | 소스 LoC | 비중 | 비고 |
|------|---------|------|------|
| `autoreply/` | 16,655 | 24.6% | 게이트웨이 최대 모듈, 분리 검토 필요 |
| `rpc/` | 6,845 | 10.1% | 메서드별 파일 분리는 양호 |
| `chat/` | 5,360 | 7.9% | LLM 통합, 도구 실행 |
| `cron/` | 4,570 | 6.8% | 스케줄링 + 마이그레이션 |
| `server/` | 4,746 | 7.0% | HTTP + WS + 라이프사이클 |

---

## 3. 코드 품질 (8.5/10)

### 강점

- **기술 부채 제로**: Go, Rust 전체에서 TODO/FIXME/HACK/XXX가 **0건**
- **에러 처리**: `fmt.Errorf` 379회(74개 파일), `context.Context` 511회(122개 파일) — Go 관용구 준수
- **Rust 안전성**: `#![deny(clippy::all)]`, `thiserror` 구조화 에러, 제네릭 패닉 최소
- **FFI 패닉 방지**: `ffi_catch()` 래퍼가 모든 `extern "C"` 함수를 `catch_unwind`로 보호
- **크기 제한 일관 적용**: FFI 입력 16MB, WebSocket 256 클라이언트, RPC body 1MB
- **Pre-commit 하드 게이트**: `make check` = `proto-check → error-code-sync → rust-fmt → rust-clippy → rust-test → cli-fmt → cli-clippy → cli-test → go-vet → go-test`

### 대형 파일 (주의)

| 파일 | LoC | 평가 |
|------|-----|------|
| `core-rs/core/src/lib.rs` | 1,775 | FFI 진입점 모음 — 구조상 자연스러운 크기 |
| `agent-runtime/model/selection.rs` | 1,678 | 모델 선택 로직 — 분리 가능 |
| `gateway-go/internal/server/server.go` | 1,594 | HTTP/WS/라이프사이클 혼재 — **분리 권장** |
| `core/markdown/parser.rs` | 1,339 | 파서 — 단일 책임이므로 적절 |
| `autoreply/commands_handlers.go` | 1,084 | 핸들러 집합 — 도메인별 분리 가능 |

---

## 4. 테스트 (8/10)

### Go 테스트 커버리지 (파일 기준, 소스:테스트 비율)

| 등급 | 모듈 (20개) |
|------|------------|
| **100%** | agent, approval, auth, daemon, dedupe, device, events, hooks, maintenance, media, middleware, monitoring, node, process, secret, skill, talk, timeouts, transcript, usage |
| **70-84%** | channel (83%), provider (84%), session (83%), telegram (70%) |
| **45-60%** | autoreply (50%), chat (45%), config (60%), cron (60%), server (60%), skills (45%), llm (60%) |
| **<40%** | **rpc (18%)**, plugin (31%), ffi (42%), vega (33%) |

### Rust 테스트

- 109개 `.rs` 파일 중 **75개(69%)**에 `#[cfg(test)]` 인라인 테스트 존재
- 4개 workspace crate: `cargo test --workspace`로 통합 실행
- Fuzz 테스트: `make go-test-fuzz` (프레임 파싱)

### 테스트 인프라

- Go: race detector 활성화 (`go test -race`), fuzz 테스트 지원
- Rust: clippy deny all + cargo test
- CI: `make check`로 전체 체크 (proto + Rust + Go + CLI)

### 개선 필요

- **rpc/ (18%)**: 43개 소스에 8개 테스트 — 130+ RPC 메서드 단위 테스트 부족
- **plugin/ (31%)**: 16개 소스에 5개 테스트 — 플러그인 로딩/디스커버리 커버리지 부족
- **autoreply/ (50%)**: 최대 모듈(16K LoC)이면서 절반만 커버

---

## 5. 보안 (9.5/10)

### FFI 보안

- 모든 `unsafe extern "C" fn`에 null 포인터 체크, 길이 검증, UTF-8 검증
- `ffi_catch()` 래퍼로 Rust 패닉 → Go abort 방지 (FFI_ERR_PANIC = -99)
- FFI 에러 코드 상수가 `core-rs/core/src/lib.rs` ↔ `gateway-go/internal/ffi/errors.go`에서 동기화
- `error-code-sync` 스크립트로 CI에서 일관성 검증

### 보안 프리미티브

| 함수 | 용도 |
|------|------|
| `constant_time_eq` | 비밀 비교 타이밍 공격 방지 |
| `sanitize_html` | XSS 방지 |
| `is_safe_url` | SSRF 보호 |
| `validate_session_key` | 세션 키 검증 |
| `sanitize_control_chars` | 제어 문자 제거 |
| `validate_frame` | 게이트웨이 프레임 구조 검증 |
| `validate_params` | RPC 파라미터 스키마 검증 |

### CI 보안 계층

1. `detect-secrets`: 시크릿 감지 (baseline 기반)
2. `zizmor`: GitHub Actions 보안 감사
3. `CodeQL`: 코드 보안 분석
4. `shellcheck`: 셸 스크립트 린팅
5. `detect-private-key`: 개인키 커밋 방지

---

## 6. CI/CD (9/10)

### 워크플로우 구성

| 워크플로우 | 역할 |
|-----------|------|
| `ci.yml` | 메인 CI: 타임아웃 15분, docs-only 스킵, 캐싱 |
| `multi-lang.yml` | Rust + Go + CLI 빌드/테스트 |
| `proto-check.yml` | 프로토 생성 + breaking change 감지 |
| `codeql.yml` | 보안 분석 |
| `workflow-sanity.yml` | 워크플로우 자체 검증 |
| `release-please.yml` | Release Please 자동 릴리스 |
| `labeler.yml` | 25K+ 자동 라벨링 규칙 |

### `make check` 체인

```
proto-check → error-code-sync → rust-fmt → rust-clippy → rust-test
→ cli-fmt → cli-clippy → cli-test → go-vet → go-test
```

---

## 7. 유지보수성 (8.5/10)

### 강점

- **CLAUDE.md 65KB**: 모든 AI 에이전트를 위한 포괄적 개발 가이드 (아키텍처, 빌드, 테스트, 코딩 스타일, 보안, 릴리스)
- **Pure-Go 폴백**: Rust 빌드 환경 없이도 `go build -tags no_ffi`로 개발/테스트 가능
- **크로스 컴파일**: CLI 5개 타깃 (linux x64/arm64, darwin x64/arm64, windows x64)
- **dev 모드**: `make go-dev` — SIGUSR1으로 자동 재시작 (exit code 75)
- **Makefile 오케스트레이션**: 30+ 타깃으로 Rust + Go + CLI + Proto 빌드 통합
- **DGX Spark 전용 빌드**: `make gateway-dgx` (Vega FTS + ML + CUDA)

### 개선 여지

- `autoreply/` 모듈이 16K+ LoC(90 파일)로 전체의 1/4 — 서브패키지 분리 검토
- `server.go` 1,594줄 — HTTP 라우팅, WS 핸들링, 서버 라이프사이클을 별도 파일로 분리 가능
- CODEOWNERS 파일 부재 (단일 개발자이므로 현재는 불필요하나 협업 확장 시 필요)

---

## 핵심 강점 (Top 5)

1. **극도의 미니멀 의존성** — Go 외부 deps 3개, 공급망 공격 표면 최소
2. **FFI 안전성 업계 최고 수준** — 패닉 복구, 크기 제한, null/UTF-8 검증, 에러 코드 동기화
3. **기술 부채 제로** — TODO/FIXME/HACK이 117K LoC 전체에서 0건
4. **TS→Go/Rust 마이그레이션 완료** — `src/` 완전 제거, 단일 런타임(Go) + 연산 커널(Rust)
5. **보안-우선 설계** — SSRF, XSS, 타이밍 공격, DoS, 프레임 검증 모두 Rust로 방어

## 개선 권장사항 (Top 5)

1. **rpc/ 테스트 보강** (18% → 60%+): 130+ RPC 메서드의 단위 테스트 추가
2. **autoreply/ 모듈 분리**: 16K+ LoC를 도메인별 서브패키지(commands, agents, dispatch, pipeline)로 분할
3. **server.go 분리**: 1,594줄을 HTTP routing / WebSocket handling / server lifecycle으로 분할
4. **plugin/ 테스트 보강** (31% → 60%+): 플러그인 로딩/디스커버리/훅 커버리지 확대
5. **model/selection.rs 분리**: 1,678줄 모델 선택 로직을 전략 패턴으로 분할
