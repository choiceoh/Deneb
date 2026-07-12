# Gateway Server 조립 변경 지도

이 패키지는 HTTP 서버와 RPC·채팅·백그라운드 능력을 조립하는 composition
root다. 제품 규칙을 구현하는 곳이 아니라, 소유 패키지의 좁은 계약을 연결하고
서버 수명주기를 관리하는 곳이다.

## 진입점과 책임

- `server.go`의 `Server`, `New`, `ServerTransport`, `ServerRPC`, `ServerRuntime`이
  최상위 수명주기와 의존 그룹을 정의한다.
- `server_lifecycle.go`의 `Server.StartAndListen`과 `Server.ShutdownCtx`가 시작,
  종료, drain 경계를 소유한다.
- `server_http_routing.go`의 `Server.buildMux`가 HTTP route의 단일 등록 지점이다.
  네이티브 클라이언트·앱 업데이트·Fleet·MCP route와 CORS 조립은
  `../gatewayhttp/routes.go`의 `gatewayhttp.RegisterRoutes`,
  `RegisterFleetAlertRoute`, `WithCORS`가
  소유하며, server는 좁은 `gatewayhttp.Config`만 채운다. SparkFleet webhook의
  decode·loopback 검증은 `../fleetapi/alert_hook.go`, 공용 cooldown은
  `../proactive/alert_gate.go`가 소유하고 server는 같은 gate 인스턴스만 주입한다.
- `gateway_hub.go`의 `Server.buildHub`가 RPC 서비스 컨테이너를 만든다.
- `method_registry.go`의 `Server.registerEarlyMethods`,
  `method_registry_late.go`의 `Server.registerLateMethods`,
  `server_rpc_session.go`의 `Server.registerSessionRPCMethods`가 단계별 RPC 배선을
  소유한다.
- `server_workflow_side_effects.go`의 `Server.registerWorkflowSideEffects`는 RPC가
  아닌 자율 작업과 notifier의 최종 연결 지점이다.
- `../modelmaintenance/suite.go`의 `modelmaintenance.New`가 모델 튜닝,
  회귀 감시, 선택적 압축 튜너의 생성 순서와 관측 adapter를 소유한다. server는
  `Suite.Tasks()`를 등록하고 `Suite.PromptTuner()`만 RPC에 노출한다.

## 의존 방향과 불변조건

- 이 패키지는 composition root라 많은 구현을 볼 수 있지만, 비즈니스 판단을
  추가하지 않는다. 판단은 domain/pipeline/platform 소유 패키지로 이동하고
  서버에는 생성·주입·수명주기 호출만 남긴다.
- RPC 등록 순서는 Builtin → Early → Session → Late → SideEffects로 고정이다.
  chat 의존 핸들러를 Session 이전에 등록하면 안 된다.
- `Server.ShutdownCtx`에서 파생하지 않은 장기 goroutine을 만들지 않는다.
  새 작업은 종료 신호, recover, bounded drain을 반드시 소유한다.
- handler는 `GatewayHub` 전체가 아니라 각 handler의 `Deps` 계약을 받는다.
  hub에 새 비즈니스 메서드를 추가하거나 별도 배선 위치를 만들지 않는다.
- route와 RPC method 이름은 각각 `buildMux`와 method registry가 단일 소스다.
  다른 초기화 파일에서 중복 등록하지 않는다.
- 모델 유지관리 task를 server에서 직접 생성하지 않는다. task 활성화 조건,
  순서, 저장소와 telemetry adapter 변경은 `runtime/modelmaintenance`에서 한다.

## 변경과 검증

배선 변경은 생성 실패, 정상 시작, shutdown, required-method parity를 관찰하는
테스트를 함께 갱신한다. 이 패키지의 집중 검증 명령은 다음과 같다.

`cd gateway-go && go test ./internal/runtime/server`

RPC 또는 HTTP 표면을 바꿨다면 추가로 관련 handler 패키지 테스트를 실행하고,
백그라운드 작업 변경은 취소된 context에서 종료되는지 테스트로 증명한다.
모델 튜닝·회귀 감시·압축 튜너 배선을 바꿨다면
`go test -race ./internal/runtime/modelmaintenance ./internal/runtime/server`로
task 순서와 RPC에 노출되는 tuner 인스턴스가 같은지 함께 검증한다.
클라이언트 HTTP 조립을 바꿨다면
`go test ./internal/runtime/gatewayhttp ./internal/runtime/nativeapi ./internal/runtime/server`
로 method boundary와 인증 전 CORS preflight를 함께 확인한다.
