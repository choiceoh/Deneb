# Chat Prompt 변경 지도

이 패키지는 system prompt 조립, workspace context 파일 로딩, session-frozen
snapshot과 prompt token budget을 소유한다. chat turn은 완성된 prompt를
소비하며 cache 구획과 context 신뢰 경계를 다시 조립하지 않는다.
세부 cache 정책의 정본은 `docs/agent-rules/prompt-cache.md`다.

## 진입점과 책임

- `system_prompt_params.go`의 `SystemPromptParams`, `ToolDef`,
  `RuntimeInfo`가 prompt 입력 계약이다.
- `system_prompt.go`의 `BuildSystemPrompt`와
  `BuildSystemPromptBlocks`가 static, semi-static, dynamic 구획을 조립한다.
- `context_files.go`의 `ContextFile`, `LoadContextFiles`,
  `WithSessionSnapshot`, `ClearSessionSnapshot`이 workspace 문서 탐색과
  session 고정 snapshot을 소유한다.
- `prompt_cache.go`의 `PromptCache`와 package singleton `Cache`가
  static prompt, context mtime, session/topic snapshot을 한 lock 체계로
  관리한다.
- `budget.go`의 `PromptBudget`, `PromptFragment`,
  `PromptBudget.Optimize`가 선택 가능한 prompt 조각의 token 예산을 소유한다.
- `topic_knowledge.go`의 `LoadTopicKnowledge`와
  `PersonaCacheKeyFor`가 topic/persona 내용을 cache key에 결합한다.

## 의존 방향과 불변조건

- 의존 방향은 `chat pipeline → prompt → llm/tokenest/propus`다. prompt에서
  chat handler, tools, runtime server를 import하지 않는다.
- `BuildSystemPromptBlocks`는 system cache marker를 최대 2개만 사용하고
  나머지 2개를 trailing message에 남긴다. 새 `cache_control`을 추가하지
  않는다.
- static 또는 semi-static 내용에 영향을 주는 입력은 cache key에도 반드시
  반영한다. 내용만 바꾸고 key를 유지해 다른 session에 stale prompt를
  재사용하지 않는다.
- per-turn 가변 정보는 static 구획에 넣지 않는다. session 중 context 파일은
  `WithSessionSnapshot`으로 고정하고 reset 시 `ClearSessionSnapshot`으로
  함께 제거한다.
- `PromptCache` lock 순서는 `ctxMu → sessMu`다. 반대 순서로 잡거나 이미
  `ctxMu`를 가진 `LoadContextFiles` 안에서 재진입하지 않는다.
- context 파일은 byte budget, UTF-8 경계와 trust 표기를 보존한다. 파일
  전체를 무제한 system prompt에 삽입하지 않는다.

## Local change scope

prompt 조립 변경은 cache 구획 경계에 가둔다. turn 실행·도구 배선을 여기로 끌어오지 않는다.

- 함께 바꿔도 되는 이웃: `internal/pipeline/chat`(턴 소비자),
  `docs/agent-rules/prompt-cache.md`(정책 정본). `BuildSystemPrompt`/
  `PromptCache`/`LoadContextFiles` 계약이 바뀌면 `system_prompt_test.go`와
  `prompt_cache_test.go`를 먼저 본다.
- 건드리지 말 것: `internal/pipeline/chat/tools` 구현,
  `internal/runtime/server` registry, per-turn 가변 바이트를 system 구획에
  넣는 변경. prompt에서 chat handler·tools·server를 import하지 않는다.
- 집중 검증: `cd gateway-go && go test -count=1 ./internal/pipeline/chat/prompt`

## 집중 검증

prompt 변경은 block 순서·marker 수·cache key 변화·session snapshot 안정성과
budget truncation을 검증한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/prompt`
