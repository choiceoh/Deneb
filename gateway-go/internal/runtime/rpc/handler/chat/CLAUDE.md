# Chat RPC handler 변경 지도

이 패키지는 chat pipeline의 좁은 port를 `chat.*`와 `miniapp.*` RPC method로
변환한다. 실제 agent turn, OCR/ASR/document extraction, work-feed 저장은 주입된
의존성이 소유하며 handler는 검증과 wire 변환만 담당한다.

## 진입점과 소유권

- `chat.go`의 `ChatHandler`, `Deps`, `Methods`, `BtwMethods`가 표준
  `chat.send/history/abort/steer/btw` handler map을 만든다.
- `miniapp_bridge.go`의 `MiniappMethods`가 native-client request/response
  bridge와 optional capture method 등록을 소유한다. native-client session
  기본값과 delivery channel 계약은 `pipeline/chatport`가 소유한다.
- `miniapp_workfeed.go`는 work-feed feedback/rewrite adapter를 제공한다. method
  이름의 최종 등록 위치는 server composition root의 `method_registry*.go`다.

## 의존 방향과 불변조건

- 의존 방향은 `runtime/server → handler/chat → chatport + domain ports +
  rpcutil`이다. handler는 concrete `pipeline/chat`나 `GatewayHub`를 import하지
  않고 필요한 능력을 `Deps`로 받는다.
- `Methods`와 `MiniappMethods`는 `ChatReady`인 typed port가 없으면 method를
  노출하지 않는다. OCR/ASR/translation/work-feed 같은 optional method도 해당
  dependency가 있을 때만 등록한다.
- 빈 native session key는 `chatport.DefaultNativeSessionKey`로 blocking/streaming
  모두 `client:main`으로 정규화해야 한다. RPC 오류는 `rpcerr` code와 원인
  chain을 보존한다.
- capture, feed correction, rewrite처럼 외부 문서·메일을 agent turn에 넣는 경로는
  `GateUntrustedTools=true`를 반드시 유지한다. ephemeral side action은 visible chat
  transcript에 남기지 않으며 빈 rewrite로 기존 card를 지우지 않는다.
- raw capture persistence 실패는 로그로 드러내되 이미 가능한 chat reply를
  유실시키지 않는다.

## 테스트와 집중 검증

- `contracts_test.go`의 `TestMethodsAndMiniappMethodsNilContract`,
  `TestCaptureDocumentValidationContract`, `TestWorkfeedFeedbackMessageContract`가
  registration과 입력 경계를 고정한다.
- `chat_port_test.go`의 `TestChatMethodsRejectTypedNilPort`와
  `capture_document_card_test.go`의 `TestCardCapturedDocumentCreatesCardAndSkipsDuplicates`가 typed port와
  deliverable 중복 방지를 검증한다.

`cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/chat`
