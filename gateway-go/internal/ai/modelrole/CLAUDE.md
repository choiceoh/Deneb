# Model Role 변경 지도

이 패키지는 제품 임무의 역할(main, tiny, lightweight, coding, fallback,
vision)을 실제 provider/model과 LLM client로 해석한다. 호출자는 역할만
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
- `thinking.go`의 `ThinkingOffExtraBody`와
  `Registry.ThinkingOffExtraBodyFor`가 raw LLM 호출의 thinking-off
  request shape 단일 소스다.
- `health.go`의 `Registry.RecordModelFailure`,
  `Registry.RecordModelSuccess`, `Registry.ModelUnhealthy`가 fallback
  circuit breaker를 소유한다.

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
- raw-call thinking shape를 호출자마다 복제하지 않는다. 새 template toggle은
  `ThinkingOffExtraBodyFor`와 routing capability를 함께 갱신한다.

## 집중 검증

역할 변경은 기본값, opt-in 부재, fallback 순서, provider override, client
재해석과 health half-open을 테스트한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/ai/modelrole`
