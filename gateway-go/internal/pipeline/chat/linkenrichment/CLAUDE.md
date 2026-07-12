# Link enrichment 변경 지도

이 패키지는 inbound 사용자 메시지에 포함된 링크를 제한된 예산 안에서
보강하는 전체 비동기 수명주기를 소유한다. chat root는 실행 gate와 표시
sanitize adapter만 제공한다.

## 계약

- `engine.go`의 `Engine.Start`가 URL 추출, 중복 제거, bounded 병렬 fetch와
  panic 격리를 시작하고 `Join`이 완료·취소·fallback 결과를 결정한다.
- HTML/YouTube 변환과 total/per-link budget은 한 엔진 안에서 일관되게 적용한다.
- briefcase mode와 caller-owned history gate는 상위 chat이 판단한다. 엔진은
  현재 사용자 메시지의 already-enriched marker와 link 유무만 판단하며
  session/briefcase 구현을 import하지 않는다.
- 새 goroutine에는 recover와 취소 경계가 있어야 하며, 실패한 보강이 원래
  사용자 입력을 잃게 해서는 안 된다.

## 집중 검증

`cd gateway-go && go test -race -count=1 ./internal/pipeline/chat/linkenrichment ./internal/pipeline/chat`
