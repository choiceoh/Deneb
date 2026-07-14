# Gmail API client 변경 지도

이 패키지는 Google Gmail REST API의 인증 transport와 mailbox read/write 계약을
소유한다. 로컬 수신 archive는 `platform/mailarchive`, 분석과 사용자 표시 판단은
상위 pipeline의 책임이다.

## 진입점과 소유권

- `client.go`의 `Client`, `DefaultClient`, `Client.AccountEmail`이 OAuth token
  source, bounded HTTP client, 재시도 가능한 singleton 생성을 관리한다.
- `operations.go`의 `Client.SearchPage`, `Client.GetMessage`,
  `Client.GetThread`, `Client.GetAttachment`가 read API와 Gmail payload 변환을
  소유한다.
- `send_labels.go`의 `Client.Send`, `Client.Reply`, `Client.Trash`,
  `Client.ModifyLabels`가 mutation API를 소유하고, `message_body.go`와
  `format.go`는 각각 MIME body 추출과 transcript 표시를 담당한다.

## 의존 방향과 불변조건

- 의존 방향은 `pipeline/platform consumers → platform/gmail → googleoauth +
  httputil`이다. gmail은 mail analysis, RPC handler, local archive를 import하지
  않는다.
- 모든 API 요청은 caller context와 bearer token source를 통과하고 외부 JSON
  response를 `maxAPIResponseBytes`로 반드시 제한한다. token refresh 실패와
  persist 실패를 성공으로 숨기지 않는다.
- `SearchPage`의 metadata fan-out은 quota-safe concurrency로 제한하되 결과 순서는
  Gmail list 순서를 보존한다. 한 metadata 실패는 같은 위치의 명시적 실패 stub로
  남긴다.
- `messageToDetail`은 Gmail `ThreadID`뿐 아니라 `Message-ID`, `References`,
  `In-Reply-To`를 보존해 LMTP archive와 동일한 thread/dedup identity를 유지한다.
- Reply는 원본 thread ID와 Message-ID를 MIME `In-Reply-To`/`References`에 함께
  넣어야 하며 label 이름은 mutation 전에 실제 ID로 해석한다.

## 테스트와 집중 검증

- `client_test.go`의 `TestValidToken_RefreshesExpired`,
  `TestRefresh_SavesRotatedRefreshTokenToDisk`, `TestGetClient_RetriableOnFailure`가
  인증 수명주기를 검증한다.
- `operations_test.go`의 `TestMetadataConcurrency`, `TestCollectAttachments_ParsesAttachmentMetadata`,
  `TestBuildMIME_FormatsReplyHeaders`가 fan-out, MIME, thread 계약을 고정한다.

`cd gateway-go && go test -count=1 ./internal/platform/gmail`
