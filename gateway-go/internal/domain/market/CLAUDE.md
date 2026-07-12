# market (시장 시세) 지도

모닝레터가 쓰는 시장 시세 도메인이다. 시세 조회/캐시와, LLM이 숫자를 직접
옮겨 적지 않게 하는 digit-free 레터 토큰 치환을 소유한다.

## 진입점과 책임

- `market.go` — `Quote`, `Cache`, `NewCache`: 시세 스냅숏과 TTL 캐시.
- `letter_tokens.go` — `RecordLetterTokens`, `SubstituteLetterTokens`:
  레터 본문에 심어진 토큰을 발송 직전에 실제 수치 문자열로 치환한다.

## 의존 방향과 불변조건

- 레터의 숫자는 반드시 토큰 치환으로만 들어간다 — LLM이 수치를 직접 쓰게
  두면 안 된다(실측 사고: usd_krw 1530.98이 "1,331원"으로 배달됨). 토큰 어휘를
  바꾸면 레터 프롬프트(위키 발송 규칙)와 같은 방향으로 움직여야 한다.
- 만료된 토큰 값은 그럴듯한 옛 숫자 대신 대시(—)로 강등된다 — 신선도 위장
  금지.
- domain 계층이다: runtime을 임포트하지 않고, 시세 소스 접근은 주입된
  클라이언트로만 한다.

## 변경과 검증

`cd gateway-go && go test ./internal/domain/market`

토큰 치환 규칙 변경은 `letter_tokens_test.go`(미지 토큰·만료·fast path)와
`market_contract_test.go` 계약 단언을 함께 갱신한다.
