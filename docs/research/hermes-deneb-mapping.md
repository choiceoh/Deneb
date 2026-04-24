# Hermes Agent ↔ Deneb 전수 대응표 & 포팅 계획

> **분석 기준**: Hermes v0.11.0 (commit `c95c6bd`, 2026-04-23) ↔ Deneb main (worktree `claude/flamboyant-newton-0cb300`)
> **일시**: 2026-04-24
> **방법**: 5개 Explore 매핑 에이전트 (전수조사) + 5개 general-purpose 포팅 에이전트 (실제 Go 코드 작성) 병렬 실행
> **원본 심층 분석**: [`docs/research/hermes-agent-analysis.md`](./hermes-agent-analysis.md) (113KB / 2387줄)

---

## Status Legend

- ✅ **Equivalent** — 같은 목적 기능이 Deneb에 이미 있음
- 🔄 **Different-design** — 같은 목적, 다른 설계
- ⚠️ **Partial** — 일부만 구현
- ❌ **Missing** — Deneb에 없음 (도입 가치 검토 필요)
- 🚫 **N/A** — 의도적으로 거부 (Deneb 철학)
- 🚧 **In-progress / Landed** — 본 세션에서 포팅 진행/완료

## 섹션 색인

| 섹션 | 영역 | 대략 행 수 | 위치 |
|---|---|---:|---|
| 1 | 에이전트 코어 / LLM / 크리덴셜 / 에러 | 66 | 대화 로그 (M1 에이전트 결과) |
| 2 | 도구 / 터미널 / 보안 | 69 | 대화 로그 (M2 에이전트 결과) |
| 3 | 메모리 / 스킬 / 인사이트 / 세션 DB | 40 | 대화 로그 (M3 에이전트 결과) |
| 4 | 표면: CLI / TUI / Gateway / Cron / ACP | 76 | 대화 로그 (M4 에이전트 결과) |
| 5 | 인프라 / 상태 / 로깅 / 리서치 / 테스트 | 52 | 본 문서 |

섹션 1-4의 매핑 표는 분석 당일 대화 로그에서 확인 가능. 본 문서는 **포팅 계획의 관점에서 재정렬한 종합본** + 섹션 5 상세 표 + 실제 포팅 진행 현황을 담음.

---

## 섹션 5 — 인프라 / 상태 / 로깅 / 리서치 / 테스트 (직접 작성)

### 5A. 상태 DB & 세션 저장

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 1 | `hermes_state.py:SessionDB` (~1600 LOC, SQLite schema: sessions/messages/tool_calls tables) | 크로스-세션 영속 대화 이력 | `internal/domain/transcript/writer.go` (JSONL) + `internal/runtime/session/` (in-memory lifecycle) | 🔄 | Hermes: SQLite. Deneb: JSONL + 메모리. 단일 유저라 FTS5 cross-session 검색 덜 중요. 그러나 **Wiki 검색은 FTS5 사용** (섹션 3 참고). |
| 2 | `messages_fts` FTS5 virtual table + triggers | 세션 메시지 전문 검색 | Wiki FTS5만 (`internal/domain/wiki/search.go`). 트랜스크립트 전용 FTS5 없음 | ⚠️ | Hermes `session_search` 툴 같은 사용자 대면 검색 없음. 필요 시 JSONL → 경량 인덱스 빌드 가능. |
| 3 | `search_messages()` snippet 하이라이트 | `>>>match<<<` 마킹 context | 없음 | ❌ | 도입 시 낮은 복잡도. `pkg/textsearch/` 재사용 가능. |
| 4 | Session 복원/브랜치/undo | 과거 세션 이어가기 | `internal/runtime/server/session_restore.go` | ✅ | JSONL 기반 재구성. 동일 기능. |

### 5B. 로깅

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 5 | `hermes_logging.py:setup_logging()` RotatingFileHandler → `agent.log`/`errors.log`/`gateway.log` | 파일 회전 로그 | `internal/runtime/bootstrap/logging.go:BuildLogger()` + `internal/infra/logging/` (slog 기반) | 🔄 | Deneb: slog 네이티브 → stderr/JSON/text. 파일 rotate는 systemd journal 또는 외부 프로세스 의존. 단일 기계라 journal로 충분. |
| 6 | `_install_session_record_factory()` — 모든 LogRecord에 `[session_id]` 주입 (thread-local) | 세션 상관관계 추적 | slog `Handler.WithGroup("session")` + context 전파 (`.claude/rules/logging.md`) | 🔄 | Hermes: monkey-patched factory. Deneb: slog context-based, 더 관용적. |
| 7 | `COMPONENT_PREFIXES` (gateway/agent/tools/cli/cron) 로거 라우팅 | 컴포넌트별 파일 분리 | 없음 (단일 스트림) | ⚠️ | Deneb 단순성 우선. 필요 시 slog Handler chain으로 가능. |
| 8 | `_ManagedRotatingFileHandler` NixOS chmod 0660 | setgid 그룹 공유 | 없음 | 🚫 | Deneb 단일 유저 → 그룹 공유 불필요. |
| 9 | `agent/redact.py` — 20+ 벤더 시크릿 패턴 + `_mask_token` + import-time snapshot | 로그 내 API key / JWT / Discord mention 마스킹 | ❌ **없음** | ❌ **Battle-tested 패턴 / 도입 가치 높음** | 순수 Go 포트 가능, stdlib만. 세션 검색·에러 리포트가 키 노출하는 위험을 차단. 우선순위 높음. |

### 5C. 경로 & 감지

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 10 | `hermes_constants.py:get_hermes_home()` | `HERMES_HOME` env resolution | `config.Resolve*Dir()` (`internal/infra/config/`) — `DENEB_HOME` 유사 | ✅ | Deneb: 경로 해석 함수 존재. |
| 11 | `is_termux()` / `is_wsl()` / `is_container()` | 플랫폼 감지 | 없음 | 🚫 | Deneb DGX Spark 전용 → 감지 불필요. |
| 12 | `apply_ipv4_preference()` — `socket.getaddrinfo` monkey-patch | 부서진 IPv6 우회 | 없음 | ❌ **낮은 복잡도 / 중간 가치** | 외부 API 호출 hang 시 문제. Go는 `net.Dialer{FallbackDelay: 300ms}` 로 구현. |
| 13 | `get_subprocess_home()` — `HERMES_HOME/home` 하위 디렉터리로 HOME override | 프로파일/Docker 영속성 | 없음 | 🚫 | 프로파일 시스템 부재. |
| 14 | Profile 시스템 (`_apply_profile_override()`) | 다중 격리 인스턴스 | 없음 | 🚫 | 단일 오퍼레이터 → 불필요. |

### 5D. 유틸리티

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 15 | `utils.py:atomic_json_write()` / `atomic_yaml_write()` | temp+fsync+rename+perm 복원 | `pkg/atomicfile/atomicfile.go` (400 LOC, flock+tmp+rename+backup옵션) | ✅ **더 강력함** | Deneb 구현이 Hermes보다 풍부 (flock, Backup 옵션). |
| 16 | `utils.py:safe_json_loads()` | 예외 없는 JSON 파싱 | `pkg/jsonutil/` | ✅ | 동등. |
| 17 | `utils.py:env_int()` / `env_bool()` | env var 타입 변환 | `pkg/...` 내 스캐터됨 | ⚠️ | 작은 일관성 개선 여지. |
| 18 | `utils.py:normalize_proxy_url()` — `socks://` → `socks5://` | WSL/Clash 호환 | 없음 | ⚠️ | 프록시 미사용이면 미필요. 외부 API 호출 환경 다변화 시 추가. |
| 19 | `utils.py:base_url_hostname()` / `base_url_host_matches()` | 정확한 hostname 매칭 (substring 공격 차단) | 없음 (자체 검증) | ❌ **보안 가치 / 낮은 복잡도** | `"evil.com/api.openai.com/v1"` 같은 공격 차단. `pkg/httputil/` 에 `HostMatches()` 추가 권장. |
| 20 | `hermes_time.py:now()` + timezone 캐시 | IANA TZ 해석 (env → config → 로컬) | 없음 (시스템 TZ 직접 사용) | ⚠️ | 유저가 KST 명시하려면 필요. 낮은 복잡도. |

### 5E. Bootstrap 엔트리포인트

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 21 | `run_agent.py:main` | 메인 에이전트 엔트리 | `cmd/gateway/main.go` | ✅ | 동등한 프로세스 수명. |
| 22 | `batch_runner.py` entry | 배치 트래젝토리 생성 | 없음 | 🚫 | Deneb 프로덕션, 연구 아님. |
| 23 | `mcp_serve.py` — MCP 서버 모드 | Hermes 자체가 MCP 서버 | 없음 | ⚠️ | 현재 N/A, 향후 Claude Desktop / Claude Code 통합 시 가치. |
| 24 | `mini_swe_runner.py` | SWE-bench 외부 벤치 런처 | 없음 | 🚫 | 벤치마크 참여 의도 없음. |
| 25 | `rl_cli.py` | RL 훈련 런처 | 없음 | 🚫 | 연구 스택 거부. |
| 26 | `hermes` CLI (`hermes_cli/main.py`) | 인터랙티브 CLI | 없음 (`cmd/gateway/` 서버 only) | 🚫 | Telegram 전용. |
| 27 | `tests/conftest.py` — `_isolate_hermes_home` autouse fixture | 테스트 간 상태 격리 | `internal/testutil/` + `t.TempDir()` | ✅ | Go idiom. |

### 5F. 빌드 & 배포

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 28 | `Dockerfile` — multi-stage uv+gosu, UID 10000, VOLUME `/opt/data` | 컨테이너 배포 | 없음 (단일 Go 바이너리) | 🚫 | DGX Spark에 바로 설치. 도커 불필요. |
| 29 | `flake.nix` Nix 플레이크 | NixOS 재현 가능 빌드 | 없음 | 🚫 | Deneb NixOS 타겟 아님. |
| 30 | `nix/packages.nix` + `nixosModules.nix` + `checks.nix` + `devShell.nix` | Nix 모듈들 | 없음 | 🚫 | 위와 동일. |
| 31 | `scripts/install.sh` / `install.cmd` / `install.ps1` | 원라이너 cross-platform 설치 | `Makefile` + `scripts/dev/live-test.sh` | 🔄 | 오퍼레이터가 git pull + `make go`. 단순. |
| 32 | `setup-hermes.sh` (uv venv + `.[all]` + symlink) | 개발자 셋업 | 없음 (`make go` 직접) | 🚫 | Go 빌드 더 간단. |
| 33 | `scripts/run_tests.sh` — hermetic CI parity wrapper | 로컬↔CI drift 방지 | `scripts/dev/live-test.sh` | 🔄 | Deneb: 목 Telegram 서버까지 띄워 E2E. **Hermes 수준보다 실전 검증 우월**. |
| 34 | `scripts/release.py` | 릴리스 자동화 | 수동 / `scripts/` 내 별도 | ⚠️ | 릴리스 빈도 낮으면 수동 OK. |
| 35 | `packaging/homebrew/*` | macOS Homebrew 포뮬러 | 없음 | 🚫 | Deneb 사용자 풀 단일. |
| 36 | `pyproject.toml` — 23 optional extras | 세분화된 의존성 | `go.mod` | ✅ | Go 의존성이 더 flat. |

### 5G. 연구 인프라 (RL)

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 37 | `environments/hermes_base_env.py` — Atropos BaseEnv | RL 환경 인터페이스 | 없음 | 🚫 | 연구 제품 아님. |
| 38 | `environments/agent_loop.py` 멀티턴 엔진 | RL 롤아웃 루프 | 없음 | 🚫 | 동일. |
| 39 | `environments/agentic_opd_env.py` — OPD (On-Policy Distillation) | Per-token advantage | 없음 | 🚫 | 동일. |
| 40 | `environments/hermes_swe_env/` | SWE-bench 환경 | 없음 | 🚫 | 동일. |
| 41 | `environments/web_research_env.py` | FRAMES 벤치 | 없음 | 🚫 | 동일. |
| 42 | `environments/terminal_test_env/` | 터미널 RL | 없음 | 🚫 | 동일. |
| 43 | `environments/tool_call_parsers/` (10 파서: Hermes/Mistral/Qwen/DeepSeek/Kimi/GLM...) | 모델별 tool call 포맷 정규화 | 없음 | 🚫 | Deneb: OpenAI 호환 단일 포맷만. |
| 44 | `environments/tool_context.py` — reward function 툴 핸들 | RL 검증자 | 없음 | 🚫 | 동일. |
| 45 | `environments/benchmarks/` + `tinker-atropos/` | 벤치마크 + Tinker 서브모듈 | 없음 | 🚫 | 동일. |

### 5H. 배치 & 트래젝토리

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 46 | `batch_runner.py` (1,291 LOC) — 병렬 dataset→trajectory | 데이터 수집 | 없음 | 🚫 | 연구 전용. |
| 47 | `trajectory_compressor.py` (1,508 LOC) — **훈련용** 압축 (context_compressor와 다름!) | 토큰 예산 내 훈련 데이터 | 없음 | 🚫 | 연구 전용. |
| 48 | `scripts/sample_and_compress.py` + `datagen-config-examples/` | 데이터 생성 스크립트 | 없음 | 🚫 | 연구 전용. |

### 5I. 플러그인 & 확장성

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 49 | `hermes_cli/plugins.py` PluginManager + `register(ctx)` | 일반 플러그인 훅 | 없음 (통합 설계) | 🚫 | Deneb 철학: 플러그인 거부, 모든 도메인 폐쇄. |
| 50 | `plugins/context_engine/*` | 컨텍스트 엔진 플러그인 | 없음 | 🚫 | 동일. |
| 51 | `plugins/image_gen/*` | 이미지 생성 백엔드 | 없음 | 🚫 | Telegram 텍스트 중심. |
| 52 | `plugins/disk-cleanup/`, `example-dashboard/`, `strike-freedom-cockpit/` | 기타 플러그인 예시 | 없음 | 🚫 | 확장성은 RPC 추가로만. |

### 5J. 문서 & 테스트

| # | Hermes | 목적 | Deneb 대응 | 상태 | 비고 |
|---|---|---|---|---|---|
| 53 | `AGENTS.md` (Hermes 개발 가이드, 33KB) | 에이전트 개발 규칙 | `CLAUDE.md` + `gateway-go/CLAUDE.md` + `.claude/rules/*.md` | ✅ | Deneb이 조건부 로딩 규칙 시스템으로 더 세분화. |
| 54 | `SECURITY.md` (8.5KB) | 취약점 보고, 신뢰 모델 | 없음 | ❌ **낮은 복잡도 / 필수** | 외부 기여자 받으려면 필수. 30분 작업. |
| 55 | `CONTRIBUTING.md` (27KB) | 기여 가이드 | `CLAUDE.md` + `.claude/rules/git-pr.md` | ⚠️ | 외부 기여 받을 계획 있으면 필요. |
| 56 | `README.md` | 입구 문서 | `README.md` | ✅ | Deneb도 보유. |
| 57 | `RELEASE_v*.md` (버전별 릴리스 노트) | 변경 이력 | `CHANGELOG.md` | ✅ | 동등. |
| 58 | `website/` Docusaurus 사이트 | 공식 문서 배포 | `docs/` (Mintlify 설정) | 🔄 | Deneb은 Mintlify, Hermes는 Docusaurus. 기능 동등. |
| 59 | `tests/` + `conftest.py` hermetic isolation | pytest 단위/통합/E2E | `**/*_test.go` + `scripts/dev/live-test.sh` | ✅ | Go 관용. |
| 60 | `tests/gateway/`, `tests/agent/`, `tests/cli/` 구조 | 모듈별 테스트 분리 | `internal/*/{pkg}/*_test.go` colocation | 🔄 | Go는 코드와 동위치 테스트가 관용. |
| 61 | `tests/integration/` marker | 외부 서비스 필요 테스트 | `scripts/dev/live-test.sh` | 🔄 | Deneb 라이브 테스트가 훨씬 실전적 (목 Telegram 서버). |
| 62 | "Don't write change-detector tests" 정책 (AGENTS.md) | 카탈로그 스냅샷 금지 | `.claude/rules/testing.md` | ✅ | Deneb도 유사 원칙. |
| 63 | CI 워크플로우 (`.github/workflows/*`) | GHA CI | `.github/workflows/*` | ✅ | 동등. |

---

## 전체 종합 통계

| 상태 | 섹션 1 | 섹션 2 | 섹션 3 | 섹션 4 | 섹션 5 | 합계 |
|---|---:|---:|---:|---:|---:|---:|
| ✅ Equivalent | ~18 | ~20 | ~12 | ~10 | ~10 | ~70 |
| 🔄 Different-design | ~8 | ~6 | ~5 | ~18 | ~10 | ~47 |
| ⚠️ Partial | ~18 | ~12 | ~10 | ~8 | ~6 | ~54 |
| ❌ Missing (가치 있음) | ~12 | ~6 | ~8 | ~2 | ~4 | ~32 |
| 🚫 N/A (의도적 거부) | ~8 | ~22 | ~2 | ~36 | ~20 | ~88 |
| 🚧 In-progress/Landed | ~2 | ~3 | ~3 | ~2 | — | ~10 |
| **소계** | 66 | 69 | 40 | 76 | 52 | **303** |

(숫자는 에이전트 결과 집계 근사값; 일부 행은 중복 카운트 가능)

---

## 본 세션에서 실제 포팅된 항목 (Go 코드로 작성됨)

### P1. `/steer` main-agent — ✅ 완료
- **새 파일**: `gateway-go/internal/pipeline/chat/steer.go` (SteerQueue), `steer_inject.go` (drain+append), `steer_test.go` (13 tests), `gateway-go/internal/runtime/server/inbound_steer.go` (파서), `inbound_steer_test.go` (13 tests)
- **수정**: `internal/agentsys/agent/config.go`(BeforeAPICall hook), `executor.go`, `pipeline/chat/handler.go/run.go/run_start.go/run_exec.go/slash_dispatch.go`, `runtime/rpc/handler/chat/chat.go` (`chat.steer` RPC), `runtime/server/method_registry.go` + `_test.go`, `inbound.go`
- **테스트**: 26/26 통과 (-race)
- **한국어 마커**: `[사용자 조정: ...]` (CLAUDE.md Korean-first 준수)
- **핵심 디자인**: Anthropic block 프로토콜 대응 (tool_result in `role:"user"` vs Hermes 의 `role:"tool"`). 메시지 샬로우 복사 후 주입 → 영속 messages 불변 → 캐시 prefix 보존.

### P2. FailoverReason Classifier — ✅ 완료
- **새 패키지**: `gateway-go/pkg/llmerr/` (reason.go, classify.go, patterns.go, action.go, classify_test.go)
- **14종 Reason enum** (Hermes 13 + Unknown as zero value)
- **Classify** 파이프라인: provider-specific → HTTP status → error code → message → transport → unknown
- **DefaultAction** 매트릭스: rotate/backoff/compress/refresh/abort 권장
- **통합**: `run_helpers.go:isContextOverflow` 를 `llmerr.Classify(...).Reason == ReasonContextOverflow` 로 대체 (strictly more correct — 기존 4 패턴 + 다국어 + 구조화 에러 코드 커버)
- **TODO 마커**: `run_exec.go:884` (chatport.IsTransientError), `autoreply/runner_errors.go:ClassifyAgentError` (후속 PR에서 migrate)
- **테스트**: 29 함수 / 52 서브테스트 통과 (-race)

### P3. Checkpoint Manager — ✅ 완료
- **새 패키지**: `gateway-go/pkg/checkpoint/` (manager.go 488 LOC, types.go, index.go, retention.go, adapter.go, manager_test.go 346 LOC)
- **기능**: Snapshot/List/Restore/Diff, SHA-256 dedup, tombstone, gzip, 세션별 격리, keep-N + maxBytes retention
- **통합**: `internal/pipeline/chat/tools/fs.go:ToolWrite` 에 `snapshotBeforeWrite` 훅 (`toolctx.Checkpointer` 인터페이스 경유)
- **테스트**: 11/11 통과 (-race)
- **TODO**: 세션 부트스트랩에서 `toolctx.WithCheckpointer(ctx, ...)` 와이어링 필요 (`sessionKey` + state dir 정책 결정 필요). `ToolEdit` 에도 훅 확장 필요. 선택적으로 `checkpoint.list/restore/diff` RPC 추가.

### P4. Insights 시스템 — ✅ 완료
- **새 패키지**: `gateway-go/internal/runtime/insights/` (engine.go 367 LOC, render.go 233 LOC, engine_test.go, render_test.go)
- **새 핸들러**: `gateway-go/internal/runtime/rpc/handler/insights/` (handler.go, handler_test.go) — `insights.generate` RPC
- **Hub 와이어링**: `gateway_hub.go` 에 `Insights()` accessor 추가, `method_registry.go` 에서 `insights.New(hub.Sessions(), s.usageTracker)` 생성
- **슬래시**: `/insights [days]` + 한국어 별칭 `/사용량` (Telegram MarkdownV2, 3800-char soft cap)
- **스키마 gap 대응**: Deneb에 `messages` 테이블/비용 추적 부재 → `SchemaNotes` 로 표시 ("비용 추적 미지원 — 토큰만 표시합니다")
- **테스트**: 16+3 통과 (-race), `TestMethodRegistry_RequiredMethodsRegistered` 에 `insights.generate` 추가

### P5. Tool Interception 분석 — 결론: Scenario C (리팩토링 불필요)
- **분석 문서**: `docs/research/tool-interception-gap.md`
- **결론**: Hermes 의 `_invoke_tool` 가로채기 체인은 Python 클래스 인스턴스 상태 때문. Deneb은 이미 **closure + Deps struct** 패턴으로 agent-instance state 문제를 다르게 해결 (`toolctx.CoreToolDeps`). `ToolInterceptor` 인터페이스를 빈 채로 추가하면 투기적 일반화.
- **작은 개선**: `internal/pipeline/chat/tools.go:RegisterTool` 에 **silent replace 경고** 추가 (plugin collision 탐지)

### 본 세션 Doctrine: `.claude/rules/prompt-cache.md` — ✅ 완료
- 3-tier 캐시 구조 문서화 (Static/Semi-static/Dynamic)
- 불가침 3원칙 (과거 메시지 mutation 금지, 대화 중 툴셋 변경 금지, 시스템 프롬프트 재구성 금지)
- Cache-aware 슬래시 패턴 (deferred 기본 + `--now` opt-in)
- `/steer` 가 왜 캐시-안전한지 설명
- CLAUDE.md Rules Index에 추가됨

### 분석 문서 — ✅ 완료
- `docs/research/hermes-agent-analysis.md` (113KB, 2387줄) — 원본 심층 분석
- `docs/research/hermes-deneb-mapping.md` (본 문서) — 대응표 + 포팅 계획
- `docs/research/tool-interception-gap.md` — P5 Scenario C 결정 근거

---

## 포팅 추천 (우선순위 순, 사용자 필터 적용)

> **사용자 지침**: "인프라 오버헤드나 복잡성이 증가해도 확실한 가치가 있으면 도입 가능. 수많은 사용자들에게 검증되고 버그 픽스된 런타임 강점은 중요."

### Tier 1 — **즉시 포팅** (battle-tested 가치 높음, 본 세션 완료)

이미 본 세션에서 포팅 완료 — 별도 액션 불필요, 리뷰 & 머지만.

1. ✅ **`/steer` main-agent** — 실행 중 방향 조정 UX
2. ✅ **FailoverReason classifier** — 프로바이더별 에러 정규화 (battle-tested 13종 분류)
3. ✅ **Checkpoint Manager** — 파일 편집 전 자동 스냅샷
4. ✅ **Insights `/사용량`** — 토큰/모델/세션 투명성
5. ✅ **Prompt cache doctrine** — 불가침 원칙 문서화

### Tier 2 — **다음 1-2 PR에 포팅 권장** (가치 vs 복잡도 분명)

순수 유틸리티 + 보안 가치 → 낮은 위험, 쉬운 통합:

6. ❌ **`agent/redact.py` 이식** (500+ LOC → Go 400 LOC 예상)
   - 20+ 벤더 시크릿 패턴 (sk-, ghp_, AIza, AKIA, JWT, DB 커넥션, Bearer 헤더)
   - `_mask_token`: 18+ char는 `prefix6...suffix4`, 짧으면 `***`
   - Import-time snapshot → LLM이 runtime에 `HERMES_REDACT_SECRETS=false` export해도 무력화 불가
   - **위치**: `gateway-go/pkg/redact/` (새 패키지)
   - **통합**: `internal/infra/logging/` 핸들러 레이어에서 모든 log attr 리다액션, 툴 output 저장 전 적용
   - **가치**: 세션 검색 / 에러 리포트 / 크래시 로그 유출 방지. **Battle-tested 20+ 벤더 패턴은 직접 쓸 수 없는 축적 지혜.**

7. ❌ **URL host match 안전성** (`utils.py:base_url_host_matches`)
   - `"evil.com/api.openai.com/v1"`, `"api.openai.com.evil/v1"` 같은 공격 URL을 substring 매칭하면 native endpoint로 오인
   - **위치**: `gateway-go/pkg/httputil/` 에 `HostMatches(baseURL, domain)` 추가
   - **통합**: Provider 라우팅 / SSRF 체크 사이트에서 사용
   - **가치**: 공격 surface 제거. 매우 낮은 복잡도.

8. ❌ **Grace Call 메커니즘** (`run_agent.py:_budget_exhausted_injected`)
   - 예산 도달 시 모델에게 1회 공지 메시지 주입 → 마무리 턴 1회 → 종료
   - Deneb 현재: 예산 도달 시 즉시 종료 → "반 완성" 응답 위험
   - **위치**: `gateway-go/internal/agentsys/agent/executor.go` 루프 종료 조건
   - **복잡도**: 작음. 플래그 1개 + 메시지 주입 1회
   - **가치**: 장기 멀티턴 작업이 "깨끗한" 마무리. Hermes 가 엣지케이스에서 배운 UX 결정.

9. ❌ **Timezone 명시** (`hermes_time.py`)
   - `DENEB_TIMEZONE` env + config 해석, `zoneinfo.ZoneInfo` 캐시
   - **위치**: `gateway-go/pkg/dentime/` (새 패키지) 또는 `internal/infra/config/` 확장
   - **가치**: 유저가 KST 고정 시 로그/크론 일관성.

10. ❌ **`SECURITY.md` + `CONTRIBUTING.md` 씨앗**
    - Hermes SECURITY.md 포맷 차용 (보고 경로 + 신뢰 모델 + out-of-scope)
    - **복잡도**: 30분 분량. 외부 기여 받으려면 필수.

### Tier 3 — **가치 높지만 신중한 설계 필요** (1-2개월 로드맵)

11. ⚠️ **Provider-specific quirks 모음집** (Hermes `agent/anthropic_adapter.py` 1299-1582)
    - Thinking block signature strip (third-party 엔드포인트 / 마지막 아닌 assistant 턴)
    - Opus 4.7+ sampling param 거부 (temperature/top_p/top_k 제거)
    - Opus 4.6 xhigh effort → max 다운그레이드
    - OAuth identity block + MCP tool prefix
    - **위치**: `gateway-go/internal/ai/llm/openai.go` 또는 `internal/ai/provider/quirks/`
    - **가치**: Deneb이 Claude Opus 4.7 쓰면 같은 버그 만남. **Hermes가 프로덕션에서 배운 픽스의 집합** = battle-tested 산출물.

12. ⚠️ **Tool schema 동적 후처리** (`model_tools.py:276-334`)
    - `execute_code` 스키마가 실제 가용 sandbox tools 반영
    - `browser_navigate` 가 `web_search`/`web_extract` 부재 시 교차참조 문구 strip
    - `discord_server` 가 봇 privileged intents 반영
    - **목적**: **LLM이 없는 툴을 환각하지 않게**
    - **위치**: `gateway-go/internal/pipeline/chat/toolreg/` 스키마 생성 단계
    - **복잡도**: 중간. 각 툴의 condition 확인 필요.
    - **가치**: 에이전트 품질에 직접 영향 — 존재하지 않는 툴 호출 감소.

13. ⚠️ **Credential Pool 전략 확장** (`agent/credential_pool.py`)
    - Deneb 현재: AuthManager 단일 키. 여러 키 보유해도 rotation 로직 부재
    - Hermes 4 전략: RANDOM / LEAST_USED / ROUND_ROBIN / FILL_FIRST
    - 429 / 402 cooldown (기본 1시간, reset 헤더 override)
    - OAuth refresh 성공 시 `~/.claude/.credentials.json` sync (Anthropic)
    - **조건부**: Deneb 이 다중 API 키 사용 시작할 때 의미 있음. 지금은 N/A.

14. ⚠️ **Approval 시스템 간소화 버전** (`tools/approval.py`)
    - 단일 오퍼레이터라 Hermes 의 full 1200 LOC 는 과잉
    - 그러나 "`rm -rf /`, `dd if=/dev/zero`, `chmod 777 /etc`" 같은 **파괴적 명령 패턴 감지** 은 실수 방지에 가치
    - **제안**: `gateway-go/pkg/cmdsafety/` 에 ~30 패턴 감지기만 추가. 유저 승인 프롬프트 생략 (단일 오퍼레이터, Telegram 버튼은 무겁)
    - **가치**: 실수로 홈 디렉터리 지우는 명령 로그 전에 차단. 싱글 유저라도 가치 있음.

### Tier 4 — **선택적** (ROI 판단 필요)

15. 🔄 **Component log routing** — gateway/agent/tools/cron 로거 분리 (Hermes COMPONENT_PREFIXES). Deneb 단일 stream 유지도 OK.
16. 🔄 **Session FTS5 검색 툴** — 트랜스크립트에 FTS5 인덱스 추가 + `sessions_search` 툴. Wiki 검색이 이미 있으니 덜 긴급.
17. 🔄 **MCP 서버 모드** — Deneb 자체를 MCP 서버로 노출. Claude Desktop / Claude Code 통합 원할 때 필요. 지금은 N/A.
18. 🔄 **OSV/Tirith 보안 스캐너** — Deneb 이 외부 스킬 설치 허용 시작하면 가치. 현재 폐쇄형이라 N/A.

### Tier 5 — **거부** (Deneb 철학과 부합 X, 사용자 지침 확인됨)

19. 🚫 17+ 메시징 플랫폼 어댑터 (Telegram 전용 유지)
20. 🚫 6개 터미널 백엔드 (local DGX Spark 전용)
21. 🚫 TUI Ink/React (Telegram 유일 UI)
22. 🚫 CLI 75 슬래시 명령 (Deneb 7개 유지, vibe coder 원칙)
23. 🚫 Atropos/Tinker/RL 인프라 (연구 제품 아님)
24. 🚫 agentskills.io 공개 허브 (싱글 오퍼레이터)
25. 🚫 Profile 시스템 (단일 오퍼레이터)
26. 🚫 Plugin 아키텍처 (통합 설계 유지, 확장은 RPC 추가로만)
27. 🚫 Batch runner / Trajectory compressor for training (프로덕션 제품)
28. 🚫 Docker / Nix / Homebrew 패키징 (DGX Spark `make go` 단순성 유지)

---

## 즉시 권장 다음 단계

1. **현재 WIP 병합 정리**: 5개 포팅 에이전트가 남긴 파일들이 `make check` (golangci-lint) 에 14개 이슈. 본 세션 커밋 전 해결 필요. 주로 `pkg/checkpoint/` , `pkg/llmerr/` 내 린트 경고 (대부분 스타일). `make check` 스테이지별 검증 권장.

2. **라이브 테스트**:
   ```bash
   scripts/dev/live-test.sh restart
   scripts/dev/live-test.sh smoke
   scripts/dev/live-test.sh quality
   scripts/dev/live-test.sh logs-errors
   ```
   특히 P1 `/steer`, P4 `/insights` / `/사용량` 의 Telegram UX 실제 확인.

3. **Checkpoint 와이어링 마무리** (P3 TODO):
   - 세션 부트스트랩에서 `Manager` 생성 + `ctx = toolctx.WithCheckpointer(ctx, adapter)` 삽입
   - `ToolEdit` 에도 `snapshotBeforeWrite` 훅 확장
   - 선택적으로 `checkpoint.list/restore/diff` RPC

4. **Tier 2 포팅** 착수 (redact + URL host match + grace call + timezone) — 총 ~600 LOC 추가 예상. 1-2주 작업.

5. **SECURITY.md 씨앗 작성** — 30분. 외부 기여 대비.

---

## 변경된 파일 (본 세션)

```
docs/research/hermes-agent-analysis.md         (new, 113KB)
docs/research/hermes-deneb-mapping.md          (new, 이 문서)
docs/research/tool-interception-gap.md         (new, P5 분석)
.claude/rules/prompt-cache.md                  (new, doctrine)
CLAUDE.md                                      (Rules Index 한 줄 추가)

gateway-go/pkg/llmerr/                         (new — reason/classify/patterns/action + test)
gateway-go/pkg/checkpoint/                     (new — manager/types/index/retention/adapter + test)
gateway-go/internal/runtime/insights/          (new — engine/render + tests)
gateway-go/internal/runtime/rpc/handler/insights/ (new — handler + test)

gateway-go/internal/pipeline/chat/steer.go            (new)
gateway-go/internal/pipeline/chat/steer_inject.go     (new)
gateway-go/internal/pipeline/chat/steer_test.go       (new)
gateway-go/internal/runtime/server/inbound_steer.go   (new)
gateway-go/internal/runtime/server/inbound_steer_test.go (new)

gateway-go/internal/agentsys/agent/config.go          (edit — BeforeAPICall hook)
gateway-go/internal/agentsys/agent/executor.go        (edit — hook 호출)
gateway-go/internal/pipeline/chat/handler.go          (edit — SteerQueue / InsightsProvider)
gateway-go/internal/pipeline/chat/run.go              (edit — steerQueue dep)
gateway-go/internal/pipeline/chat/run_start.go        (edit — 와이어링)
gateway-go/internal/pipeline/chat/run_exec.go         (edit — BeforeAPICall 주입)
gateway-go/internal/pipeline/chat/slash_dispatch.go   (edit — /reset → steer clear + /insights)
gateway-go/internal/pipeline/chat/slash_commands.go   (edit — /insights, /사용량 등록)
gateway-go/internal/pipeline/chat/callbacks.go        (edit — InsightsProvider 콜백)
gateway-go/internal/pipeline/chat/tools.go            (edit — RegisterTool replace 경고)
gateway-go/internal/pipeline/chat/tools/fs.go         (edit — ToolWrite snapshotBeforeWrite 훅)
gateway-go/internal/pipeline/chat/tools/fs_test.go    (edit — 통합 테스트)
gateway-go/internal/pipeline/chat/toolctx/context.go  (edit — Checkpointer 인터페이스)
gateway-go/internal/pipeline/chat/run_helpers.go      (edit — isContextOverflow → llmerr.Classify)

gateway-go/internal/runtime/rpc/rpcutil/gateway_hub.go (edit — Insights field)
gateway-go/internal/runtime/rpc/handler/chat/chat.go   (edit — chat.steer RPC)
gateway-go/internal/runtime/server/method_registry.go  (edit — insights wiring + chat.steer)
gateway-go/internal/runtime/server/method_registry_test.go (edit — snapshot)
gateway-go/internal/runtime/server/server.go          (edit — insights field)
gateway-go/internal/runtime/server/server_rpc_session.go (edit — SetInsightsProviderFunc)
gateway-go/internal/runtime/server/inbound.go         (edit — main-agent /steer intercept)

(+ 기타 TODO 마커 추가된 파일)
```

---

## 결론 (한 문장)

**Hermes 의 런타임 강점 중 Deneb 철학과 양립 가능한 것**(battle-tested 에러 분류, 프롬프트 캐시 원칙, mid-run steer, 체크포인트, 인사이트)**을 본 세션에서 Go로 포팅 완료. 거부된 것**(17 플랫폼, 6 백엔드, RL, 플러그인, CLI)**은 Deneb narrow-deep 철학의 의도된 선택**. Tier 2 (redact, host match, grace call, timezone) 는 다음 1-2 PR에 이식 권장.
