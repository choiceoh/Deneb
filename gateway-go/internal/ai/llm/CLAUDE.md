# LLM Wire Client 변경 지도

이 패키지는 OpenAI 호환 API와 Anthropic Messages API를 하나의 요청·스트림
계약으로 정규화하는 전송 계층이다. 모델 역할 선택과 provider catalog 해석은
`ai/modelrole`이 소유하며, 이 패키지는 이미 해석된 endpoint와 model을 받아
HTTP wire 형식만 책임진다.

## 진입점과 책임

- `client.go`의 `Client`, `NewClient`, `DoStream`이 HTTP 수명주기,
  인증 header, retry와 request timeout을 소유한다.
- `types.go`의 `ChatRequest`, `Message`, `ContentBlock`, `StreamEvent`가
  provider와 무관한 공개 계약이다.
- `openai.go`의 `Client.StreamChat`이 API mode를 분기한다. OpenAI 응답은
  `openai_stream.go`에서 내부 `StreamEvent` 형식으로 변환한다.
- `openai_complete.go`의 `Client.Complete`는 짧은 동기 호출의 단일
  진입점이고 Anthropic mode에서는 streaming 경로를 재사용한다.
- `normalize.go`의 `NormalizeMessages`, `DropEmptyMessages`,
  `ContentToBlocks`가 provider 전송 전 메시지 정규화를 소유한다.
- `sse.go`의 `ParseSSE`, `parseSSE`, `startSSEPipelineWithByteLimit`가
  SSE framing과 취소 가능한 parser 수명주기를 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 `modelrole/pipeline → llm → httpretry/modelcaps`다.
  `llm`에서 `modelrole`, chat pipeline, runtime server를 import하지 않는다.
- OpenAI와 Anthropic 어느 mode든 호출자에게는 Anthropic-style
  `StreamEvent` 수명주기를 반환한다. provider별 event를 agent loop로
  누출하지 않는다.
- `DoStream`이 반환한 body는 반드시 닫혀야 한다. streaming helper의
  cancel/close/join 순서를 우회하는 별도 goroutine을 만들지 않는다.
- 빈 답, 중간 stream error, refusal, max-token truncation은 성공으로
  반환하지 않는다. 부분 결과를 nil error와 함께 커밋하면 안 된다.
- wire 변환 전 `NormalizeMessages`와 Anthropic의 `DropEmptyMessages`
  계약을 보존한다. tool use/result 순서와 content block을 문자열로 평탄화하지
  않는다.
- 구체 모델 이름이나 역할 정책을 여기 추가하지 않는다. 새 provider mode는
  `Client`의 mode 분기와 공통 request/event 계약을 함께 검증한다.

## 집중 검증

전송 변경은 request body/header, 정상 종료, provider error, 취소와 premature
EOF를 해당 `*_test.go`에서 함께 확인한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/ai/llm`
