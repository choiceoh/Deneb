# replytokens (응답 토큰 규약) 지도

침묵 응답(NO_REPLY)·응답 태그·하트비트 트리거 토큰의 **단일 규약 소스**다.
파이프라인 곳곳(전달 억제, transcript 정리, 하트비트 판정)이 같은 의미를
보도록 파싱/판정/제거를 한곳에 모은다. 패키지명은 `tokens`.

## 진입점과 책임

- `tokens.go` — 침묵 응답 판정: `IsSilentReplyText`, `IsSilentReplyPrefixText`,
  `StripSilentToken`.
- `reply_tags.go` — 응답 태그: `ReplyTag`, `ExtractReplyTags`, `StripReplyTags`,
  `HasReplyTag`, `ReplyTagValue`, `ApplyReplyThreading`(reply-to 스레딩 해석).
- `heartbeat.go` — 하트비트 토큰: `ResolveHeartbeatPrompt`,
  `IsHeartbeatContentEffectivelyEmpty`, `StripHeartbeatToken`
  (`StripHeartbeatMode`/`StripHeartbeatResult`).

## 의존 방향과 불변조건

- core 리프 패키지다: pipeline/runtime을 임포트하지 않는다. 소비자 →
  replytokens 단방향 의존만 허용.
- 토큰 문자열 의미는 시스템 프롬프트의 규칙과 쌍이다 — 판정 로직을 바꾸면
  프롬프트 쪽 규칙 문구와 반드시 같은 커밋에서 검토한다(한쪽만 바꾸면 침묵
  응답이 사용자에게 새거나, 실응답이 억제된다).
- Strip 계열은 멱등해야 한다: 이미 제거된 텍스트에 다시 적용해도 결과가
  변하지 않는다.

## 변경과 검증

`cd gateway-go && go test ./internal/core/replytokens`

판정 경계 변경은 `tokens_test.go`·`heartbeat_test.go`의 에지 단언(접두/공백/
대소문자)과 `contracts_test.go`를 함께 갱신한다.
