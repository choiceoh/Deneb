# Model Role 변경 지도

이 패키지는 제품 임무의 역할(main, tiny, tinyfallback, lightweight, coding,
fallback, vision)을 실제 provider/model과 LLM client로 해석한다. 호출자는 역할만
선택하며, 현재 배치와 fallback·capability·health 정책은 이 패키지가 소유한다.
임무별 역할 정책의 정본은 `docs/agent-rules/model-roles.md`다.

## 진입점과 책임

- `registry.go`의 `Role`, `Registry`, `RegistryOptions`,
  `NewRegistryWithOptions`가 역할 매핑과 client cache의 진입점이다.
- `Registry.ResolveModel`, `Registry.Client`, `Registry.FallbackChain`이
  호출자가 사용하는 해석 계약이다.
- `capability.go`의 `Registry.CapabilityForModel`,
  `Registry.ProfileForModel`, `Registry.RefreshVllmRole`이 builtin,
  config override, live vLLM discovery의 계층화를 소유한다.
- `profile.go`의 `ProfileFor`는 sampling/reasoning builtin 표,
  `routing.go`의 `Registry.RoutingProfileForModel`은 effort routing
  override를 소유한다.
- `thinking.go`의 `ThinkingOffDirectiveFor`와
  `Registry.ThinkingOffDirectiveFor`가 raw LLM 호출의 thinking-off
  request shape 단일 소스다. 지시는 두 종류다: 템플릿 kwarg(vLLM 계열)와
  `DisablesReasoningParam`(오픈라우터 `reasoning.enabled=false`). 본문 조립은
  어댑터가 `llm.ThinkingOffFields` 한 곳으로 한다 — 어댑터가 kwarg 만 옮기면
  오픈라우터에 `{"chat_template_kwargs":{"":false}}` 가 샌다.
- `RoleTinyFallback`(`agents.tinyFallbackModel`)은 opt-in tiny 전용 1순위
  폴백이다. tiny 체인에만 끼고, thinking 강제 off 는 tiny 와 같다.
- `Registry.UnmeteredFallbacks`는 역할 클라이언트를 **직접 쥐고** 체인을 걷지
  않던 호출자(메일 stage-1 추출·결재 비용 추출, 위키 질의 확장)에게 주는 체인이다.
  종량제 칸(웜홀 `metered` 표시, 그리고 웜홀을 안 거치는 오픈라우터의 `:free` 아닌
  모델 — `modelcaps.OpenRouterPaid`)은 시도하지 않고 뺀다 — 폴백이 없던 경로에
  폴백이 생기면서 과금이 시작되면 안 된다(도그마 #7). `buildClient`는 오픈라우터 provider 클라이언트에
  `llm.WithReasoningParam`을 건다.
- `health.go`의 `Registry.RecordModelFailure`,
  `Registry.RecordModelSuccess`, `Registry.ModelUnhealthy`가 fallback
  circuit breaker를 소유한다. `Registry.SetEngineDown`·`Registry.EngineDown`은
  readiness 프로브(`internal/ai/enginelive`)가 알린 **엔진 다운 집합**이다 — 스트릭 없이
  `ModelUnhealthy`를 참으로 만들고, 집합에서 빠지는 모델은 스트릭도 지운다
  (복구 직후 쿨다운 동안 폴백에 묶이지 않게). `buildClient`가 이 판정을 모든
  레지스트리 클라이언트에 게이트로 건다(`llm.WithBackendDownCheck`).

## 의존 방향과 불변조건

- 의존 방향은 `runtime/pipeline → modelrole → llm/modelcaps/router`다.
  registry는 server나 chat pipeline을 import하지 않는다.
- 제품 코드는 concrete model ID가 아니라 `Role`을 선택한다. 새 임무나 역할
  변경은 `docs/agent-rules/model-roles.md`의 근거와 함께 갱신한다.
- `RoleCoding`과 `RoleVision`은 opt-in이다. 미설정 역할을 빈
  `ModelConfig`로 삽입해 configured로 보이게 만들지 않는다.
- capability 우선순위는 builtin → provider override → vLLM live window다.
  live discovery network 호출은 `Registry.mu` 밖에서 수행한다.
- `Registry.mu`와 `health.mu`는 독립 lock이며 동시에 잡지 않는다.
  client cache나 role 변경 경로에서 이 순서를 깨지 않는다.
- raw-call thinking 정책을 호출자마다 복제하지 않는다. 새 template toggle은
  `ThinkingOffDirectiveFor`와 routing capability를 함께 갱신하고, wire map 조립은
  각 transport adapter에 둔다.

## 집중 검증

역할 변경은 기본값, opt-in 부재, fallback 순서, provider override, client
재해석과 health half-open을 테스트한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/ai/modelrole`
