# localcal (로컬 일정 스토어) 지도

게이트웨이 자체 일정 스토어다. 네이티브 클라이언트에서 사용자가 직접 추가한
일정을 `{stateDir}/calendar.json`에 영속화한다 — Google OAuth 쓰기 스코프
없이도 생성/수정/삭제가 완전 동작하게 하는 존재 이유를 가진다. 읽기 경로에서
핸들러가 읽기전용 Google 일정과 병합한다.

## 진입점과 책임

- `store.go` — 전부가 여기 있다: `Store`, `New(path)`, `Default()`,
  `CreateInput`, 로컬 ID 판별 `IsLocalID`(접두사 기반).

## 의존 방향과 불변조건

- Google Calendar API(`platform/calendar`)를 임포트하지 않는다 — 병합은
  핸들러 계층의 일이고, 이 스토어는 로컬 파일만 안다(경계 유지).
- 영속화 실패 시 메모리 상태를 반드시 롤백한다 — 파일과 메모리가 갈라진 채
  성공을 보고하면 재시작에서 일정이 증발한다(생성·수정 모두 테스트로 고정).
- 로컬 일정 ID는 항상 `IsLocalID` 접두 규약을 따른다 — Google 이벤트 ID와의
  네임스페이스 충돌 금지. 접두사 변경은 클라이언트 분기까지 깨는 계약 변경이다.

## 변경과 검증

`cd gateway-go && go test ./internal/platform/localcal`

영속화/롤백 의미 변경은 `store_test.go`의 실패 주입 단언(생성 롤백·수정 복원)
과 `contracts_test.go`를 함께 갱신한다.
