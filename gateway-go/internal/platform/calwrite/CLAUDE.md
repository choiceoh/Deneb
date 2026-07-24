# calwrite (Google Calendar 쓰기 미러) 지도

로컬에서 만든 일정을 사용자의 Google Calendar에 **단방향(Deneb → Google)**으로
써 내보내는 패키지다. 읽기 전용 `platform/calendar` 클라이언트의 **쓰기 짝**이며,
일부러 별도 패키지로 둔다 — calendar 클라이언트의 불변조건은 "구글 쓰기 스코프
없이도 동작"이라 그 읽기 표면에 POST/PATCH/DELETE를 섞으면 안 되고, localcal
저장소는 Google API를 임포트하지 않는다. **병합/미러 오케스트레이션은 핸들러
계층**(handlerminiapp/schedule/calendar.go)이 로컬 쓰기 성공 후 이 패키지를 불러서 한다.

## 진입점과 책임

- `client.go` — `Client`(쓰기 클라이언트), `DefaultSyncer`, `Insert/Patch/Delete`
  (Calendar v3 `/calendars/primary/events`), `writeEnabled`(env 게이트), `APIError`.
- `syncer.go` — `Syncer`: localID→googleID 매핑을 소유(`{stateDir}/calendar-sync.json`
  영속)하고 `Push`(신규=POST·기존=PATCH)/`Remove`(DELETE+언매핑)/`MirroredGoogleIDs`
  (읽기 dedup용) 오케스트레이션. `remote` 인터페이스로 테스트에서 HTTP 없이 로직 검증.
- `event.go` — `calendar.Event` → Calendar v3 이벤트 본문 변환(종일=date 경계·시간=dateTime),
  `denebLocalId` extended property로 구글 측에 Deneb 저작 이벤트임을 표시.

## 활성화 (기본 OFF · 운영자 수동)

1. **env**: `~/.deneb/.env`에 `DENEB_CALENDAR_GOOGLE_WRITE=1`. 미설정이면 `DefaultSyncer`가
   에러를 반환해 핸들러가 로컬 전용으로 그대로 degrade(기존 동작과 동일).
2. **OAuth 쓰기 스코프 재동의**: 현재 `~/.deneb/credentials/calendar_token.json`은 읽기
   전용(`calendar.readonly`)일 확률이 높다. 쓰기는 `https://www.googleapis.com/auth/calendar.events`
   스코프로 **브라우저 동의를 다시 받아** 같은 파일을 재발급해야 한다(읽기 클라이언트와 토큰 공유).
   토큰 발급 플로우는 레포 밖(수동)이며, 재발급 후엔 읽기/쓰기 클라이언트 양쪽이 같은 토큰을 쓴다.

## 불변조건

- **best-effort**: 구글 쓰기 실패는 로컬 쓰기를 절대 실패시키지 않는다(로컬 = 진실원).
  실패는 `Syncer.warn`(핸들러가 주입한 `slog.Warn`)에 남기고, 핸들러는 반환 에러를 무시한다.
- **경계**: 이 패키지는 platform 계층 — domain/runtime 역임포트 금지. `calendar.Event`
  타입만 platform/calendar에서 가져온다(읽기 클라이언트의 API 호출은 사용 안 함).
- **단일 사용자**: `Syncer.mu` 하나가 id-map과 (드문) 네트워크 호출을 함께 보호해 미러가
  직렬화된다. 외부 콜백(warn)은 락 해제 후에만 호출.
- **read+write 동시 활성 시 dedup**: 미러된 이벤트는 구글 읽기에도 잡히므로, 핸들러의
  `dropMirrored`가 `MirroredGoogleIDs`로 구글 사본을 제거해 로컬 사본과 중복 표시를 막는다.

## 변경과 검증

`cd gateway-go && go test ./internal/platform/calwrite`

쓰기 표면(Insert/Patch/Delete)이나 매핑 의미를 바꾸면 `client_test.go`(httptest 요청 계약)와
`syncer_test.go`(매핑·영속·best-effort)를 함께 갱신하고, 핸들러 오케스트레이션 계약은
`handlerminiapp/schedule/calendar_writer_test.go`까지 실행한다. 실제 Google 쓰기 라이브
검증은 운영자 OAuth 쓰기 재동의가 선행돼야 한다.
