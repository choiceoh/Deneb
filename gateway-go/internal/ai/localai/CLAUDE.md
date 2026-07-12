# Local AI hub 변경 지도

이 패키지는 lightweight local-model 요청의 단일 admission·dispatch 경계다.
호출자는 직접 GPU endpoint를 조정하지 않고 `Hub`에 typed `Request`를 제출하며,
hub가 priority, token budget, cache, health, cancellation을 일관되게 적용한다.

## 진입점과 소유권

- `hub.go`의 `Config`, `Hub`, `New`, `Hub.Submit`, `Hub.Shutdown`이 전체
  lifecycle과 요청 pipeline을 소유한다.
- `request.go`의 `Request`, `Response`, `Priority`, `SimpleRequest`가 호출자
  계약이다. `Hub.CallLocalLLM`은 기존 좁은 함수형 port용 adapter다.
- `queue.go`는 priority/FIFO queue, `budget_limiter.go`는 in-flight token
  admission, `cache.go`는 semantic response cache, `health.go`는 bounded
  `/models` probe를 각각 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 `pipeline/domain callers → ai/localai → llm/modelrole/tokenest`다.
  localai는 chat pipeline이나 runtime server를 import하지 않으며 model 선택은
  `modelrole.Registry`에서만 가져온다.
- 요청 순서는 cache check → health gate → priority queue → token admission →
  dispatch다. 같은 priority는 FIFO이고 queue 초과 시 가장 오래된 background만
  drop하며 critical/normal 요청을 대신 버리지 않는다.
- token estimate가 priority별 전체 한도보다 크면 기다리지 말고
  `ErrRequestTooLarge`로 즉시 거절한다. critical overdraw 외에는 in-flight budget을
  넘으면 안 된다.
- caller cancellation과 `Hub.Shutdown`은 queued·active 요청과 outbound stream까지
  반드시 전파된다. 종료와 동시에 새 queue entry가 orphan되면 안 된다.
- cache key는 messages의 경계, max tokens, response format, `ExtraBody`를 포함한다.
  reasoning toggle은 `modelrole` 정본을 사용하고 caller ExtraBody가 최종 override다.

## 테스트와 집중 검증

- `contracts_test.go`의 `TestRequestQueueOrderingAndLifecycleContract`,
  `TestTokenBudgetLimiterAdmissionContract`, `TestLinkedRequestContextContract`,
  `TestCacheKeySemanticIdentityContract`가 핵심 상태 경계를 고정한다.
- `hub_test.go`의 `TestSubmit_CallerCancellationStopsActiveRequest`와
  `TestShutdownCancelsActiveRequest`가 zombie request 방지를 검증한다.

`cd gateway-go && go test -count=1 ./internal/ai/localai`
