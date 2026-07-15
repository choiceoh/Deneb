# Tool Port 계약 변경 지도

이 패키지는 chat tool 구현·등록·실행기가 함께 쓰는 가장 낮은 공유 계약
계층이다. 실행 구현·등록 정책·도메인 의존 bag을 소유하지 않고, typed
function/interface, per-turn context와 작은 동시성 안전 상태만 제공한다.

## 진입점과 책임

- `types.go`의 `ToolFunc`, `ToolDef`, `ToolRegistrar`,
  `ToolExecutor`가 구현과 registry 사이의 핵심 포트다.
- sibling `tooldeps/`의 `CoreToolDeps`, `ProcessDeps`, `SessionDeps`,
  `ChronoDeps`, `WikiDeps`, `CalendarDeps`가 composition root에서 tool
  constructor로 전달되는 의존 계약이다. 새 도메인/platform 의존은
  `toolport`가 아니라 `tooldeps`에 둔다.
- `context.go`의 `WithDeliveryContext`/`DeliveryFromContext`,
  `WithTurnContext`/`TurnContextFromContext`,
  `WithRunCache`/`RunCacheFromContext`가 typed context 접근 경계다.
- `turn_context.go`의 `TurnContext`, `NewTurnContext`,
  `TurnContext.Wait`, `DetectCycle`이 한 turn 안의 tool 결과 참조를
  조정한다.
- `run_cache.go`의 `RunCache`, `NewRunCache`, `IsCacheableTool`,
  `IsMutationTool`, `BuildCacheKey`가 run-local cache 의미를 소유한다.
- `display.go`의 `StripToolResultBlocksForDisplay`와
  `TransliterateAssistantTextForDisplay`는 transcript 원본을 바꾸지 않는
  표시용 변환이다.

## 의존 방향과 불변조건

- `toolport`는 chat subtree의 leaf다. `tools`, `toolreg`, chat root,
  lower domain/platform package를 import하지 않는다.
- `tooldeps`는 도구 wiring용 dependency bag의 소유자다. `toolport`는
  `tooldeps`를 import하지 않는다.
- context 값은 반드시 같은 파일의 `With*`/`*FromContext` 쌍으로
  읽고 쓴다. 외부 package가 key를 복제하거나 string key를 만들지 않는다.
- `TurnContext`, `RunCache`, `ToolExecStats`처럼 여러 goroutine이
  접근하는 상태는 기존 lock/atomic 경계를 유지한다. `TurnContext.Store`
  이후의 `TurnResult`는 immutable로 취급하고 새 API가 내부 map을 직접
  노출하지 않게 한다.
- mutation 분류와 cache invalidation은 한 계약이다. 새 write 도구를
  cacheable read로 분류하거나 path-scoped invalidation에서 누락하지 않는다.
- display helper는 RPC 응답용 message slice에만 적용한다.
  `StripToolResultBlocksForDisplay`는 새 slice를 만들지만 다른 helper는
  전달받은 slice를 제자리 수정할 수 있으므로 persisted message backing
  slice나 원본 tool result에 직접 호출하지 않는다.

## Local change scope

toolport는 chat subtree의 leaf 계약이다. 실행·등록·도메인 bag은 여기로 끌어오지 않는다.

- 함께 바꿔도 되는 이웃: `internal/pipeline/chat/tooldeps`(의존 bag),
  `internal/pipeline/chat/tools`·`internal/pipeline/chat/toolreg`(소비자).
  `ToolFunc`/`ToolDef`/`TurnContext`/`RunCache` 계약이 바뀌면
  `context_test.go`와 `run_cache_test.go`에서 `WithTurnContext`·`BuildCacheKey`를
  먼저 본다.
- 건드리지 말 것: `internal/pipeline/chat` root turn 오케스트레이션,
  `internal/runtime/server` registry, domain/platform import 추가.
  `toolport`에서 `tools`·`toolreg`·chat root를 import하지 않는다.
- 집중 검증: `cd gateway-go && go test -count=1 ./internal/pipeline/chat/toolport`

## 집중 검증

context 취소, concurrent wait/store, cache invalidation, defensive copy와
display 원본 불변성을 테스트한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/toolport`
