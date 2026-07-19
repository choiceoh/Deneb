# Mail Archive 변경 지도

이 패키지는 on-box IMAP archive를 두 소비 표면으로 제공한다. 메일 분석에는
thread/sender 문맥을, native mail UI에는 Gmail-like repository 계약을
제공한다. OCR·문서 추출과 분석 판단은 pipeline 계층이 소유한다.

## 진입점과 책임

- `source.go`의 `Config`, `Source`, `New`,
  `Source.RelatedMessages`가 mailanalysis용 과거 thread/sender 문맥을
  제한된 IMAP fetch로 제공한다.
- `repository.go`의 `Repository`, `RepositoryOptions`,
  `NewRepository`, `Repository.SearchPage`, `Repository.GetMessage`가
  native client용 Gmail-like 읽기 계약을 소유한다.
- `repository_search.go`가 archive query plan, mailbox scan, overlay/attachment
  filter, 정렬·page token 결정을 소유한다. 동작 변경은
  `repository_search_test.go`의 검색 특성 테스트에서 먼저 고정한다.
- `overlay/store.go`의 `overlay.Store`가 read/archive/trash와 archive locator의
  로컬 영속화를 독립 소유한다. 저장 형식·원자성 변경은 `overlay/store_test.go`에서
  먼저 고정하고 repository는 이 API를 통해서만 상태를 읽고 쓴다.
- `context.go`의 `ReadContextMessage`, `SearchContextMessages`,
  `ThreadContext`, `ProjectHistoryContext`가 bounded context API다.
- `repository_query.go`가 Gmail-style query를 IMAP criteria로 변환하고,
  `attachment.go`의 `ReadAttachment`가 raw attachment bytes를 반환한다.
- `imap.go`는 프로토콜 transport, `lmtpd.ParseDetail`은 RFC message
  parsing 경계다. 상위 API가 두 구현을 우회하지 않는다.

## 의존 방향과 불변조건

- 의존 방향은 `pipeline/runtime → platform/mailarchive → {mailarchive/overlay,
  gmail, lmtpd, mailbody}`이며 `mailarchive/overlay → pkg/atomicfile`이다.
  mailarchive에서 mailanalysis, document extractor, RPC handler를 import하지 않는다.
- archive message의 본문·첨부는 원시 데이터로 반환한다. OCR/ASR/요약을
  이 패키지에 넣어 transport와 분석 계층을 결합하지 않는다.
- thread ancestor가 sender history보다 우선하며 fetch 수와 References 탐색
  상한을 보존한다. 한 message의 실패가 무제한 scan으로 번지면 안 된다.
- imported mail의 날짜 범위는 Date header 기준 `SENTSINCE/SENTBEFORE`다. <!-- docref:ignore -->
  INTERNALDATE로 되돌리면 bulk import가 일자별 조회에서 사라진다.
  `SENTSINCE`가 서버에서 NO로 거부되면(`uidSearchSentAware`) ALL/나머지
  criteria로 재검색하고 Date header 반개구간(`[SentSince, SentBefore)`)으로
  post-filter 한다 — 깨진 ENVELOPE 한 통이 day-pager 전체를 멈추게 하지 않는다.
- 지원하지 않는 Gmail query는 bounded recent view로 명시적으로 degrade하고
  mailbox candidate scan은 상한을 지킨다. 일부 mailbox 실패는 다른 mailbox가
  완료됐을 때만 degrade하며, 전부 실패한 장애를 빈 inbox 성공으로 숨기지 않는다.
- read/archive/trash는 `overlay.Store` 계약을 통해 적용한다. IMAP
  원본 mutation으로 의미를 바꾸지 않는다.

## 집중 검증

기본 검증은 mock IMAP과 결정적 clock을 사용하며 live test는 별도 환경이
있을 때만 실행한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/platform/mailarchive`

overlay 저장 계약 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/platform/mailarchive/overlay`
