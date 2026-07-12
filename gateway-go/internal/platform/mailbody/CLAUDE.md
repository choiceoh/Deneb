# mailbody (메일 본문 정제) 지도

수신 메일 원문을 LLM 분석용/표시용으로 정제하는 결정적(비-LLM) 파이프라인이다.
서명·인용·숨김 블록 제거, 첨부 흔적 정리, 메일 날짜 파싱을 소유한다.

## 진입점과 책임

- `cleaner.go` — 공개 표면: `CleanForAnalysis`(분석용 축약),
  `CleanForDisplay`(표시용, `CleanResult`·`HiddenBlock`으로 접힌 블록 보존).
- `cleaner_strip.go` / `cleaner_cut.go` / `cleaner_signals.go` — 제거·절단·신호
  단계별 내부 분할(순수 이동, 관심사별).
- `maildate.go` — `ParseMailDate`: 다양한 Date 헤더 변형을 KST 기준으로
  정규화해 파싱한다.

## 의존 방향과 불변조건

- 결정적이어야 한다: 같은 입력은 항상 같은 출력 — LLM 호출·시계 의존을 이
  패키지에 넣지 않는다(날짜 파싱은 입력 문자열만 사용).
- `ParseMailDate`는 타임존 미표기 한국 메일을 KST로 해석한다는 계약이 있다 —
  UTC 가정으로 되돌리는 변경은 금지(과거 KST 버그 정정 이력).
- 표시용 정제는 정보를 파괴하지 않는다: 접힌 내용은 `HiddenBlock`으로 반드시
  보존해 클라이언트가 펼칠 수 있게 한다. 분석용은 축약이 허용되는 유일한 면이다.

## 변경과 검증

`cd gateway-go && go test ./internal/platform/mailbody`

정제 규칙을 바꾸면 `cleaner_test.go`의 실메일 픽스처 단언을 갱신하고, 소비자
(mailanalysis·mailwork) 테스트로 다운스트림 회귀를 확인한다.
