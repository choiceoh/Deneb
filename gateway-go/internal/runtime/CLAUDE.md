# runtime 서브트리 지도 (구조)

> 게이트웨이 런타임의 **구조적 지도** — HTTP 서버·RPC 디스패치·세션 상태기계·배경 서브시스템이 어디에 있고 어떻게 엮이는지. 배선 *정책*(GatewayHub 5규칙)은 `docs/agent-rules/hub-wiring.md`가 소관, 여기 복붙하지 않는다. 모듈 전체 맵은 상위 `gateway-go/CLAUDE.md`.

## 디렉토리 맵

| 경로 | 역할 |
|---|---|
| `server/` | HTTP+SSE 서버, RPC 등록 배선, 배경 서브시스템·태스크 (~90 소스 + ~65 테스트 파일, ↓ 파일 클러스터) |
| `rpc/` | 레지스트리 기반 RPC 디스패처. `dispatch.go`/`methods.go`/`rpc/register.go`/`workerpool.go` |
| `rpc/handler/<domain>/` | 도메인별 핸들러(agent·chat·session·skill·wiki·process·observe·insights·handlerminiapp·handlerevents·provider·system·gateway·checkpoint). `Deps` 구조체 + `Methods(deps)`만 노출 |
| `rpc/rpcutil/` | `hub/gateway_hub.go`(서비스 컨테이너 — 읽기 접근자·late-bind setter·phase 헬퍼 외 행위는 `Broadcast`/`Validate`뿐; `hub_alias.go`가 rpcutil 레벨 재익스포트), `rpcutil/helpers.go` |
| `../../core/rpcerr/`·`rpc/rpctest/` | 에러 타입 / 테스트 헬퍼 |
| `../domain/session/` | 세션 도메인 상태기계(`IDLE→RUNNING→DONE/FAILED/KILLED/TIMEOUT`), 전이 검증, 이벤트 pub/sub 버스. runtime보다 아래 계층이라 pipeline/platform도 역의존 없이 사용 |
| `bootstrap/` | 기동 시퀀스 조립 |
| `manifest/` | `/health`에 노출하는 실행 바이너리·스킬·도구·모델 구성의 비식별 SHA-256 지문 |
| `mcpapi/` | `POST /mcp` — 데네브 기억을 읽기 전용 MCP 도구로 외부 AI에 노출. **MCP 2.0(2026-07-28) 전용** — `initialize` 핸드셰이크 시대는 제거됐고 구버전 클라는 400(`-32020`/`-32022`)으로 거절된다. 유일한 예외가 `server/discover`로, 2.0 메타데이터 없이도 답해 클라가 뭘 말해야 하는지 알아낼 수 있다. `mcpapi/handler.go`(전송·툴 allowlist), `protocol2026.go`(버전 게이트·`Mcp-Method`/`Mcp-Name` 헤더 검증·`resultType`·캐시 힌트) | <!-- docref:ignore -->
| `../../infra/process/` | exec 프로세스 추적 |
| `../../core/observe/` | LogCapture ring + observe 평면([project_observe_plane]) |
| `events/`·`insights/` | 이벤트 버스 / 인사이트 집계 |

### server/ 파일 클러스터 (이름 규칙으로 읽기)

- **`server*.go`** — 서버 코어: `server/server.go`(타입), `server_http*.go`(라우팅·miniapp·cron·files·fleet·update·gzip·event_ingest), `server_lifecycle.go`, `server_rpc*.go`(RPC 등록), `server_options.go`, `server_chat_config.go`.
- **`*_subsystem.go`** — 배경 서브시스템: `autonomous`·`genesis`·`infra`·`memory`·`workflow`. 각자 PeriodicTask/배경 루프를 소유.
- **`*_task.go`** — 배경 태스크: `boot_task`·`heartbeat_task`·`goal_task`.
- **`method_registry.go`** — ★Deps 배선 단일 지점(인라인 리터럴). 어댑터 레이어 없음.
- **`rpc/rpcutil/hub/gateway_hub.go`** — `buildHub()`(유일 생성자, `rpcutil.NewGatewayHub` 래퍼).
- 나머지는 기능 단위(`miniapp_models*`·`notify_*`·`mail_*`·`wiki_*`·`workfeed_*`·`role_health_watch`·`regression_watch` 등).

## 핵심 흐름

### 인바운드 RPC
```
POST /api/v1/miniapp/rpc (server_http_miniapp.go)
  → rpc/dispatch.go (레지스트리 lookup, 스레드세이프)
  → rpc/handler/<domain>/ 의 Methods 핸들러 (Deps만 받음)
```

### 기동 시 RPC 등록 5단계 (순서 고정)
`server_rpc*.go` + `method_registry.go`:
```
registerBuiltinMethods()      # 허브 전 — 서버상태 클로저 (server_rpc.go)
registerEarlyMethods(hub)     # chatHandler 전 — ~50 도메인 인라인 (method_registry.go)
registerSessionRPCMethods()   # chatHandler 생성 (server_rpc_session.go)
registerLateMethods(hub)      # chatHandler 후 — Chat/BTW/Miniapp-chat/Exec/Wiki/Genesis/GmailAnalyze (method_registry.go; Aurora 드리밍은 SideEffects)
registerWorkflowSideEffects() # 비-RPC: autonomous/dreaming/notifier (server_rpc_session.go)
```

### 채팅 파이프라인 연결
`server/chat_pipeline.go`·`chat_manager.go`가 `pipeline/chat`을 서버에 결선한다. 챗 턴의 *내부* 흐름은 `pipeline/chat/CLAUDE.md` 참조.

## 흔한 작업 진입점

| 하려는 것 | 시작점 |
|---|---|
| 새 RPC 도메인 추가 | `docs/agent-rules/hub-wiring.md`의 3단계 — 핸들러 `Deps`+`Methods` → 허브 필드 → `method_registry.go` 인라인 배선 → `requiredMethods` 스냅샷 갱신 |
| 새 HTTP 라우트 | `server_http_routing.go`에서 시작, 핸들러는 `server_http_<area>.go` |
| 새 배경 서브시스템/주기작업 | `*_subsystem.go` 패턴 따라 신설 → `registerWorkflowSideEffects`에서 기동 |
| 세션 상태 전이 변경 | `internal/domain/session/` 상태기계 (전이 검증이 잘못된 전이를 거부) |
| 미니앱 모델 피커/헬스 | `miniapp_models*.go` |

## 하위 런타임 포트 지도

이 섹션은 server가 소비하는 runtime 하위 패키지의 좁은 진입점이다. 의존 방향은
`server/method_registry.go`와 `server/*_subsystem.go`가 아래 runtime 포트를
소비하고, 하위 패키지가 server를 역으로 import하지 않는 구조를 유지한다.

- `nativepush/client_push.go`의 `NewHub`, `PublishWithFallback`가 native-client
  SSE fan-out·FCM fallback 경계를 소유하고, `proactive/alert_gate.go`의
  `AlertGate`가 중복 relay 경계를 소유한다. push 실패는 notifier/fallback
  경로에 남기고 server 상태를 직접 조회하지 않는다.
- `meeting/meeting_harvest.go`의 `NewHarvestService`,
  `meeting/calendar_briefing.go`의 `NewCalendarBriefingService`,
  `meeting/plaud_recordings.go`의 `NewPlaudService`가 회의 수집 작업의 시작점이다.
  wiki enrichment는 포트로만 받고 calendar/provider 상태를 전역에서 재조회하지 않는다.
- `wikiwork/wiki_scout_task.go`의 `NewScoutTask`,
  `wikiwork/wiki_review_task.go`의 `NewReviewTask`,
  `wikiwork/wiki_research_task.go`의 `NewResearchTask`,
  `wikiwork/supernote_digest_task.go`의 `NewSupernoteDigestTask`가 wiki 배경 작업
  진입점이다. 파일 저장·검증 불변조건은 wiki store에 남긴다.
- `phoneevents/handler.go`의 `New`, `Handler.ServeHTTP`가 phone action 수집 경계다.
  raw notification 저장은 `../domain/phoneledger/ledger.go`의 `New`, `ReadTail`이 소유한다.
  HTTP handler는 ledger append와 async ingest 순서를 바꾸지 않는다.
- `rpc/handler/skill/skill.go`의 `Methods`,
  `rpc/handler/skill/skill_genesis.go`의 `GenesisMethods`,
  `rpc/handler/mail/analyzebind/gmail_analyze.go`의 `GmailAnalyzeMethods`,
  `rpc/handler/mail/gmailops/analysis_store.go`의 `NewAnalysisStore`는 RPC handler 포트다.
  handler는 `Deps`만 받고 `rpcutil.GatewayHub` 또는 server를 import하지 않는다.
- `briefcase/timeline.go`의 `NewTimeline`, `briefcase/device.go`의 `NewDeviceTwin`,
  `insights/engine.go`의 `New`, `insights/render.go`의 `RenderPlain`은 독립 런타임
  엔진이다. 외부 IO는 caller가 넘긴 interface 뒤에 둔다.

`cd gateway-go && go test ./internal/domain/phoneledger ./internal/runtime/proactive ./internal/runtime/meeting ./internal/runtime/wikiwork ./internal/runtime/phoneevents ./internal/runtime/rpc/handler/skill ./internal/runtime/rpc/handler/mail ./internal/runtime/briefcase ./internal/runtime/insights`

## 함정

- **배선은 `method_registry.go`에서만.** 다른 파일에서 Deps 구조체 조립 금지(예외: `server_rpc.go`의 `registerBuiltinMethods` 서버상태 클로저). 어댑터 파일(`hub_adapters.go` 류) 만들지 마라 — `docs/agent-rules/hub-wiring.md` 5규칙 + 스냅샷 테스트가 강제. <!-- docref:ignore -->
- **핸들러는 `rpcutil.GatewayHub`를 import하지 않는다.** `Deps` 구조체만 받는다. Hub는 순수 서비스 컨테이너 — 읽기 접근자·late-bind setter(`SetWikiStore` 등)·phase 헬퍼 외의 행위 메서드는 `Broadcast`/`Validate`뿐이며, 비즈니스 로직 추가 금지.
- **등록 5단계 순서 의존**: Builtin(허브 전) → Early(Chat 없음) → Session(Chat 생성) → Late(Chat 의존) → SideEffects. Chat-의존 메서드를 Early에 두면 nil. 새 단계는 정말 필요할 때만.
- **graceful shutdown drain hang 이력**(배포 후 미니앱 404): HTTP 리스너 닫혔는데 프로세스 생존 → watchdog+bound drain으로 방어([project_gateway_shutdown_wedge]). 종료 격리 kill은 `fuser`(`pkill -f`는 셸 자살).
- **배경 goroutine**은 `docs/agent-rules/concurrency.md`: `Server.ShutdownCtx()` 파생 + recover + 종료경로. 사용자 무응답 실패는 `Error`+broadcast(`docs/agent-rules/logging.md`).
- **dev 게이트웨이가 prod cron/transcripts 공유**(homeDir 기준) — 라이브 검증 후 즉시 stop([reference_livetest_dev_cron_shared]).
