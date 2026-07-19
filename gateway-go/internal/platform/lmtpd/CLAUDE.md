# lmtpd (LMTP 수신 서버) 지도

메일 수신용 LMTP 서버 플랫폼이다. 소켓 수신 → 파싱 → 디스크 큐 적재까지의
전달 경로를 소유하고, 메일 분석/분류는 소비자(mailanalysis 파이프라인)가
큐에서 꺼내 수행한다.

## 진입점과 책임

- `server.go` — `Server`, `New(addr, handler, log)`, `Handler`: LMTP 세션
  수락과 메시지 콜백.
- `parse.go` — `Message`, `ParseMessage`, `ParseDetail`: 원문 파싱과 Gmail
  상세 타입으로의 변환.
- `queue.go` — `Queue`, `NewQueue`, `QueueItem`, `QueueStats`: 디스크 기반
  수신 큐(enqueue→claim→complete).
- `systemd_socket.go` — systemd 소켓 액티베이션 수신.
- `dedup.go` / `large_attach.go` — 중복 수신 방지·대형 첨부 처리.

## 의존 방향과 불변조건

- LMTP 포트는 프로덕션 인스턴스가 단독 소유한다 — dev/live-test 인스턴스는
  mailLmtp를 반드시 끈 채 뜬다. 바인드 경합이 생기면 dev가 실메일을 삼키는
  사고가 된다(운영 불변조건).
- 큐는 at-least-once다: claim한 아이템은 complete 전에 죽으면 재클레임된다 —
  소비자는 중복 처리에 안전해야 하고, 여기서 임의로 exactly-once를 가장하면
  안 된다.
- 수신 경로는 메일 본문을 해석하지 않는다(파싱까지만) — 분석 판단을 이
  패키지에 넣지 않는다.

## 변경과 검증

`cd gateway-go && go test ./internal/platform/lmtpd`

큐 상태기계 변경은 `queue_test.go`(enqueue/claim/complete)로, 소켓 경로는
`server_test.go`(SplitListenAddr 계열)와 `systemd_socket.go`의 계약으로 고정한다. 라이브 검증 시 dev 인스턴스의 LMTP
비활성(포트 소유권)을 먼저 확인한다.
