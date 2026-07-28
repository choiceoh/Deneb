# Groupware tool 변경 지도

이 패키지는 agent-facing `groupware` 도구 구현만 소유한다. Amaranth10
전자결재·게시판·ERP 원장 조회를 `platform/groupware`에 위임하고, `area=people`
결과에 한해 wiki/org enrichment를 붙인다.

## 의존 방향과 불변조건

- 등록 정책과 schema는 `toolwire/core`와 `toolwire/schema`가 소유한다.
- 실제 headless login, HMAC, reader 실행은 `platform/groupware`가 소유한다.
- 이 패키지는 read-only tool이다. approve, post, delete 같은 mutation을 추가하지
  않는다.
- `wiki.Store`는 선택 dependency다. nil이면 people enrichment를 건너뛰고 원문
  결과를 사용자에게 돌려준다.

## 집중 검증

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/groupwareops`
