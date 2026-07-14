# Runtime Events 변경 지도

이 패키지는 게이트웨이 SSE/브로드캐스트 버스다. RPC·chat·push는 이벤트
이름과 payload만 넘기고, 연결별 fan-out·tap·구독은 여기가 소유한다.

## 진입점과 책임

- `broadcaster.go`의 `Broadcaster`, `Broadcast`, `BroadcastWithOpts`,
  `BroadcastToConnIDs`, `RegisterTap`이 fan-out과 operator tap을 소유한다.
- `event_payload.go`의 `EventPayload`, `PayloadFromRaw`, `PayloadOf`,
  `Bytes`가 패키지 경계의 불투명 JSON 계약이다. `any`/`json.RawMessage`를
  export하지 않는다.
- `publisher.go`의 `Publisher`가 session/agent/config 변경 이벤트를
  typed helper로 발행한다.
- `gateway_subscriptions.go`가 연결 수명주기에 묶인 구독 payload를 조립한다.

## 의존 방향과 불변조건

- 의존 방향은 `runtime/server|rpc → events`다. `domain/*`와
  `pipeline/chat/toolport`는 이 패키지를 import하지 않는다 — composition
  root가 `EventPayload`로 변환한다.
- tap/`Broadcast` payload는 항상 `EventPayload`다. 호출자는 `PayloadOf` 또는
  `PayloadFromRaw`로 감싼다.
- 부분 전송 실패는 에러 슬라이스로 반환하고, 이미 보낸 연결을 롤백하지
  않는다.

## 집중 검증

`contracts_test.go`, `publisher_test.go`, `broadcaster_test.go`로 fan-out·tap·
구독을 확인한다.

`cd gateway-go && go test -count=1 ./internal/runtime/events`
