# rpcerr (구조화 RPC 에러) 지도

RPC 핸들러의 구조화 에러 타입을 소유한다. flat한 `protocol.NewError(code,
message)` 대신, 세션·모델·채널 같은 컨텍스트 필드를 담아 로그와 응답이 같은
사실을 보게 한다.

## 진입점과 책임

- `error.go` — 전부가 여기 있다: `Error` 타입과 생성자 `New`, `Newf`, `Wrap`,
  편의 생성자 `MissingParam`, `InvalidParams`, 체이닝 `With...`, 그리고 wire
  응답으로의 변환(`ToShape`/`Response` 경로).

## 의존 방향과 불변조건

- core 계층 리프 패키지다: runtime/pipeline/domain을 임포트하지 않는다.
  의존 방향은 항상 핸들러 → rpcerr 단방향이다.
- 에러 코드 문자열은 클라이언트(네이티브·andromeda)가 분기하는 계약이다 —
  기존 코드 값 변경은 금지, 추가만 허용.
- `Wrap`은 원인 에러를 반드시 보존해야 한다(`Unwrap` 성립) — errors.Is/As
  체인이 끊기면 상위 재시도 분기가 침묵으로 무너진다.
- 컨텍스트 필드에 크리덴셜·토큰을 담지 않는다(로그로 직행하는 표면이다).

## 변경과 검증

`cd gateway-go && go test ./internal/core/rpcerr`

응답 변환 형태를 바꾸면 `error_test.go`의 shape 보존 단언과 핸들러 소비처
테스트를 함께 갱신한다.
