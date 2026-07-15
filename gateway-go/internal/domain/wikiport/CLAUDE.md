# Wikiport 경계 변경 지도

`wikiport`는 `internal/domain/wiki` 구현을 외부 패키지로부터 분리하는 안정
계약이다. 채팅, RPC, runtime, 메일 파이프라인은 이 패키지를 import하고,
파일 저장 규칙과 인덱스 구현은 `internal/domain/wiki` 안에 남긴다.

## 진입점과 책임

- `port.go`의 `Store`, `Config`, `Page`, `SearchResult` alias는 기존 wiki DTO를
  외부 소비자에게 노출하는 호환 계약이다. alias를 추가할 때는 호출자가 정말
  구현 타입 전체가 필요한지 먼저 좁은 interface로 표현할 수 있는지 확인한다.
- `port.go`의 `NewStore`, `NewStoreWithSearchOptions`, `NewWikiDreamer`는 wiki
  구현 생성자를 통과시키는 유일한 생성 진입점이다.
- `Tier1Store`, `RelatedSearchStore`, `WikiPageReader`, `MemoryStore` 같은
  interface는 기능별로 필요한 메서드만 담는 좁은 포트다. 새 consumer가 필요한
  메서드는 해당 consumer의 실제 동작 단위에 맞춰 별도 interface로 둔다.

## 의존 방향과 불변조건

- 의존 방향은 `runtime/pipeline/RPC -> wikiport -> domain/wiki`로만 흐른다.
  `internal/domain/wiki`가 `wikiport` 또는 runtime 계층을 import하면 안 된다.
- `wikiport`는 forwarding과 typed contract만 소유한다. 파일 경로 검증, 인덱스
  갱신 순서, project/deal 규칙 같은 도메인 불변조건은 반드시
  `internal/domain/wiki` 구현에 남긴다.
- `map[string]any` 기반 wire 스키마나 RPC 응답 조립을 이 패키지에 추가하지
  않는다. 전송 모양은 handler 계층에서 만들고, 이 경계는 compile-time 타입만
  제공한다.

## 변경과 검증

새 export를 추가하면 `port.go`에서 기존 wiki symbol을 명시적으로 alias하거나
얇게 forwarding한다. 동작 검증은 구현 패키지 테스트에 맡기고, 이 패키지는
컴파일 경계가 깨지지 않는지 확인한다.

`cd gateway-go && go test ./internal/domain/wikiport`
