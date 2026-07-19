# RPC 공용 경계 변경 지도

이 패키지는 RPC handler가 공유하는 typed request/response adapter와 서버
composition root의 서비스 컨테이너를 소유한다. 도메인 handler는 이 계약을
사용하되 `runtime/rpc` dispatcher나 전체 서버를 역으로 의존하지 않는다.

## 진입점과 소유권

- `helpers.go`의 `DecodeParams`, `RespondOK`, `Bind`, `BindCtx`,
  `BindHandler`, `BindHandlerCtx`가 JSON 경계에서 구체적인 요청·응답 타입을
  보존하고 `protocol.ResponseFrame`으로 변환한다.
- `hub/gateway_hub.go`의 `HubConfig`, `GatewayHub`, `NewGatewayHub`가 공유
  런타임 서비스를 한 번 조립한다. `GatewayHub.Validate`,
  `GatewayHub.AdvancePhase`, `GatewayHub.Broadcast`만 컨테이너 수준 동작이다.
- `HandlerFunc`와 `BroadcastFunc`는 handler 패키지들이 공유하는 정본
  시그니처다.

## 의존 방향과 불변조건

- 의존 방향은 `server/rpc handlers → rpcutil → protocol/rpcerr/runtime
  services`다. handler에는 전체 `GatewayHub`가 아니라 좁은 `Deps`를 넘기며,
  rpcutil에 비즈니스 판단을 추가하지 않는다.
- `GatewayHub` 등록 단계는 `PhaseInit → PhaseEarly → PhaseSession →
  PhaseLate` 순서만 허용한다. 시작 전 `Validate`가 모든 필수 의존성의 누락을
  한 오류에 보존해야 한다.
- `Bind*`는 decode 실패 때 handler를 호출하면 안 되고, `*rpcerr.Error`의 code와
  details를 보존하며 일반 오류만 `INVALID_REQUEST`로 변환해야 한다.
- nil request나 nil bound function은 panic하지 않고 요청 ID를 보존한 typed
  오류 응답을 반드시 반환한다.

## 테스트와 집중 검증

- `contracts_test.go`의 `TestGatewayHubValidateReportsAllMissingDependencies`,
  `TestGatewayHubAdvancePhaseRejectsOutOfOrderTransitions`, `TestGenericBindPreservesResponseJSONAndErrorShapes`,
  `TestNilBoundFunctionsReturnErrorsInsteadOfPanicking`이 수명주기와 wire 계약을
  고정한다.
- `helpers_test.go`의 `TestUnmarshalParams_InvalidJSON`과
  `TestTruncateForError`가 입력 오류와 진단 문자열 경계를 확인한다.

`cd gateway-go && go test -count=1 ./internal/runtime/rpc/rpcutil`
