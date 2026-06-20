# runtime 서브트리 지도 (구조)

> 게이트웨이 런타임의 **구조적 지도** — HTTP 서버·RPC 디스패치·세션 상태기계·배경 서브시스템이 어디에 있고 어떻게 엮이는지. 배선 *정책*(GatewayHub 5규칙)은 `.claude/rules/hub-wiring.md`가 소관, 여기 복붙하지 않는다. 모듈 전체 맵은 상위 `gateway-go/CLAUDE.md`.

## 디렉토리 맵

| 경로 | 역할 |
|---|---|
| `server/` | HTTP+SSE 서버, RPC 등록 배선, 배경 서브시스템·태스크 (124파일, ↓ 파일 클러스터) |
| `rpc/` | 레지스트리 기반 RPC 디스패처. `dispatch.go`/`methods.go`/`register.go`/`workerpool.go` |
| `rpc/handler/<domain>/` | 도메인별 핸들러(agent·chat·session·skill·wiki·process·observe·insights·handlerminiapp·handlerevents·provider·system·gateway·checkpoint). `Deps` 구조체 + `Methods(deps)`만 노출 |
| `rpc/rpcutil/` | `gateway_hub.go`(서비스 컨테이너 — `Broadcast`/`Validate`만), `helpers.go` |
| `rpc/rpcerr/`·`rpc/rpctest/` | 에러 타입 / 테스트 헬퍼 |
| `session/` | 세션 라이프사이클 상태기계(`IDLE→RUNNING→DONE/FAILED/KILLED/TIMEOUT`), 전이 검증, 이벤트 pub/sub 버스 |
| `bootstrap/` | 기동 시퀀스 조립 |
| `process/` | exec 프로세스 추적 |
| `observe/` | LogCapture ring + observe 평면([project_observe_plane]) |
| `events/`·`insights/` | 이벤트 버스 / 인사이트 집계 |

### server/ 파일 클러스터 (이름 규칙으로 읽기)

- **`server*.go`** — 서버 코어: `server.go`(타입), `server_http*.go`(라우팅·miniapp·cron·files·fleet·update·gzip·event_ingest), `server_lifecycle.go`, `server_rpc*.go`(RPC 등록), `server_options.go`, `server_chat_config.go`.
- **`*_subsystem.go`** — 배경 서브시스템: `autonomous`·`genesis`·`infra`·`memory`·`workflow`. 각자 PeriodicTask/배경 루프를 소유.
- **`*_task.go`** — 배경 태스크: `boot_task`·`heartbeat_task`·`goal_task`.
- **`method_registry.go`** — ★Deps 배선 단일 지점(인라인 리터럴). 어댑터 레이어 없음.
- **`gateway_hub.go`** — `buildHub()`(유일 생성자, `rpcutil.NewGatewayHub` 래퍼).
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
registerEarlyMethods(hub)     # chatHandler 전 — ~30 도메인 인라인 (method_registry.go)
registerSessionRPCMethods()   # chatHandler 생성 (server_rpc_session.go)
registerLateMethods(hub)      # chatHandler 후 — Chat/BTW/Exec/Aurora (method_registry.go)
registerWorkflowSideEffects() # 비-RPC: autonomous/dreaming/notifier (server_rpc_session.go)
```

### 채팅 파이프라인 연결
`server/chat_pipeline.go`·`chat_manager.go`가 `pipeline/chat`을 서버에 결선한다. 챗 턴의 *내부* 흐름은 `pipeline/chat/CLAUDE.md` 참조.

## 흔한 작업 진입점

| 하려는 것 | 시작점 |
|---|---|
| 새 RPC 도메인 추가 | `.claude/rules/hub-wiring.md`의 3단계 — 핸들러 `Deps`+`Methods` → 허브 필드 → `method_registry.go` 인라인 배선 → `requiredMethods` 스냅샷 갱신 |
| 새 HTTP 라우트 | `server_http_routing.go`에서 시작, 핸들러는 `server_http_<area>.go` |
| 새 배경 서브시스템/주기작업 | `*_subsystem.go` 패턴 따라 신설 → `registerWorkflowSideEffects`에서 기동 |
| 세션 상태 전이 변경 | `session/` 상태기계 (전이 검증이 잘못된 전이를 거부) |
| 미니앱 모델 피커/헬스 | `miniapp_models*.go` |

## 함정

- **배선은 `method_registry.go`에서만.** 다른 파일에서 Deps 구조체 조립 금지(예외: `server_rpc.go`의 `registerBuiltinMethods` 서버상태 클로저). 어댑터 파일(`hub_adapters.go` 류) 만들지 마라 — `.claude/rules/hub-wiring.md` 5규칙 + 스냅샷 테스트가 강제.
- **핸들러는 `rpcutil.GatewayHub`를 import하지 않는다.** `Deps` 구조체만 받는다. Hub는 `Broadcast`/`Validate` 외 메서드를 갖지 않는 순수 서비스 컨테이너.
- **등록 5단계 순서 의존**: Early(Chat 없음) → Session(Chat 생성) → Late(Chat 의존) → SideEffects. Chat-의존 메서드를 Early에 두면 nil. 새 단계는 정말 필요할 때만.
- **graceful shutdown drain hang 이력**(배포 후 미니앱 404): HTTP 리스너 닫혔는데 프로세스 생존 → watchdog+bound drain으로 방어([project_gateway_shutdown_wedge]). 종료 격리 kill은 `fuser`(`pkill -f`는 셸 자살).
- **배경 goroutine**은 `.claude/rules/concurrency.md`: `Server.ShutdownCtx()` 파생 + recover + 종료경로. 사용자 무응답 실패는 `Error`+broadcast(`.claude/rules/logging.md`).
- **dev 게이트웨이가 prod cron/transcripts 공유**(homeDir 기준) — 라이브 검증 후 즉시 stop([reference_livetest_dev_cron_shared]).
