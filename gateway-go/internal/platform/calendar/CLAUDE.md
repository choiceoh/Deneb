# calendar (Google Calendar 클라이언트) 지도

Google Calendar API의 얇은 읽기 중심 클라이언트다. 인증·HTTP·타입 변환만
소유하고, 일정 판단(신호 감지·회의 준비)은 소비자(heartbeat 신호 수집기,
meeting 런타임)가 한다.

## 진입점과 책임

- `client.go` — `Client`, `DefaultClient`(환경/크리덴셜에서 조립), `APIError`
  (HTTP 상태·본문을 보존하는 구조화 에러).
- `operations.go` — `Client` 메서드로 노출되는 일정 조회/조작 오퍼레이션.
- `types.go` — `Event`, `Attendee`, `ConferenceInfo` 등 API 응답 타입.

## 의존 방향과 불변조건

- 이 패키지는 platform 계층이다: domain/runtime을 임포트하지 않는 단방향
  의존만 허용된다.
- 사용자가 직접 추가하는 로컬 일정은 여기가 아니라 `internal/platform/localcal`
  소관이다 — Google 쓰기 스코프 없이도 동작해야 하는 생성/수정/삭제를 이
  클라이언트에 섞으면 안 된다(경계 유지). 핸들러가 두 소스를 병합한다.
- **Google 쓰기 미러**(로컬 일정 → 구글, 단방향)는 이 읽기 클라이언트가 아니라
  별도 `internal/platform/calwrite` 소관이다 — 같은 이유로 여기에 POST/PATCH/DELETE를
  넣지 않는다. 기본 ON(`DENEB_CALENDAR_GOOGLE_WRITE=0`으로 off), best-effort. 핸들러가
  로컬 쓰기 성공 후 calwrite를 호출한다.
- API 실패는 반드시 `APIError`로 상태·본문을 보존해 올린다 — 문자열로 눌러
  버리면 상위의 재시도/진단이 불가능해진다.

## 변경과 검증

`cd gateway-go && go test ./internal/platform/calendar`

오퍼레이션 표면을 바꾸면 `operations_test.go`·`client_test.go`를 함께 갱신하고,
소비자(heartbeat 신호 수집기)의 계약 테스트까지 실행한다.
