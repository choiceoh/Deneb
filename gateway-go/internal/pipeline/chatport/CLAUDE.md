# chatport 계약 지도

채팅 파이프라인의 **타입 경계(port) 패키지**다. 런타임(server)·도구·genesis가
chat 구현을 임포트하지 않고 대화 턴을 부르도록, 요청/응답·transcript·회상·스킬
넛지의 계약 타입만 모아 둔다. 구현 로직은 두지 않는다.

## 진입점과 책임

- `chatport.go` — 동기 턴 계약: `SyncRequest`, `SyncResult`, `DeliveryContext`,
  `ToolStreamEvent`, `TypingSignaler`, `ReplyDirectives`.
- `transcript_contracts.go` — transcript 계약: `ChatMessage`,
  `NewTextChatMessage`, `ChatAttachment`, `SearchResult`, `MatchedMsg`,
  `MarshalJSONString`.
- `native_contracts.go` — native-client chat delivery/session 계약:
  `NativeClientChannel`, `DefaultNativeSessionKey`.
- `skill_contracts.go` — genesis 넛저 계약: `SkillNudger`, `SkillNudgeSnapshot`.
- `recall_contracts.go` / `runtime_contracts.go` — 회상·런타임 어댑터 계약.
- `activation_notice.go` — deferred tool 활성화 공지 포맷/파서:
  `FormatFetchActivationNotice`, `ParseActivationNotices`,
  `ExtractActivationNotices`.
- 모델 provider 설정과 live model picker 제어 계약은 형제 leaf 패키지
  `internal/pipeline/modelport`가 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 단방향이다: 소비자(runtime/server·skilllifecycle·heartbeat) →
  chatport ← 구현(pipeline/chat). 이 패키지는 pipeline/chat 구현을 절대
  임포트하지 않는다 — 그 순간 port가 아니라 순환 경계가 된다.
- 활성화 공지는 transcript를 왕복해야 한다: `Format*Notice`가 만든 문자열은
  `ParseActivationNotices`가 반드시 되읽을 수 있어야 하고, 포맷을 바꾸면 파서와
  같은 커밋에서 움직여야 한다(단일 소스).
- 여기 타입은 wire 계약처럼 취급한다: 필드 제거·의미 변경은 소비자 전체(blast
  radius)를 확인한 뒤에만.

## 변경과 검증

`cd gateway-go && go test ./internal/pipeline/chatport`

계약 타입을 바꾸면 소비자 패키지(runtime/server, runtime/skilllifecycle,
pipeline/chat) 테스트까지 함께 실행해 경계 양쪽이 같은 의미를 보는지 확인한다.
