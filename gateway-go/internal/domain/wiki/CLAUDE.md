# Wiki 도메인 변경 지도

이 패키지는 파일 기반 위키의 저장소, 인덱스, 프로젝트·거래 도메인 규칙과
자율 정리 작업을 소유한다. RPC, 채팅 도구, 메일 파이프라인은 이 패키지의
공개 계약을 소비할 뿐 저장 파일을 직접 해석하거나 수정하지 않는다.

## 진입점과 책임

- `store.go`의 `Store`와 `NewStore`가 저장소 수명주기와 디렉터리 경계를
  정의한다. 새 영속 동작은 먼저 이 계약이 소유해야 하는지 판단한다.
- `page.go`의 `Page`와 `NewPage`는 위키 페이지의 메타데이터·본문 모델을
  소유한다. 프런트매터 해석 규칙을 호출자에 복제하지 않는다.
- `index.go`의 `Index`와 `NewIndex`는 검색 인덱스의 단일 진입점이다.
- `graph_query.go`의 `Store.GraphContext`와 `Store.PageConnections`는 그래프
  조회 계약이다. 임베딩 재정렬은 이 파일 안의 내부 단계로 유지한다.
- `dreamer.go`의 `WikiDreamer`와 `NewWikiDreamer`가 자율 정리 오케스트레이션을
  소유하고, 실제 적용은 `dreamer_apply.go`가 담당한다.
- 프로젝트 상태 변경은 `project_status.go`, 거래 레코드는 `deal_records.go`와
  `deal_records_query.go`, 연락처 보강은 `contacts.go`에서 시작한다.

## 의존 방향과 불변조건

- 위키는 도메인 계층이다. `runtime`, RPC handler, `pipeline/chat`을 import하면
  안 된다. 상위 계층이 `Store`의 좁은 메서드나 자체 인터페이스에 의존한다.
- 파일 쓰기는 반드시 `Store`가 계산한 루트 아래에서 수행한다. 호출자가
  절대 경로나 프런트매터를 직접 조립하는 경로를 추가하지 않는다.
- 인덱스와 페이지 파일은 함께 갱신되어야 한다. 페이지 쓰기 성공 후 인덱스
  갱신을 생략하면 검색·그래프 결과가 달라지므로 기존 원자적 순서를 보존한다.
- `WikiDreamer`가 제안한 변경도 결정적 guard와 경로 검증을 통과한 뒤에만
  적용한다. LLM 출력 자체를 저장 계약으로 취급하지 않는다.
- 외부 전송용 DTO는 RPC 계층에서 조립한다. 이 패키지의 도메인 타입에
  `map[string]any` 기반 wire 스키마를 추가하지 않는다.

## 변경과 검증

가장 가까운 테스트 파일에서 동일한 `Store` 또는 `WikiDreamer` 심볼을 먼저
찾아 회귀 사례를 추가한다. 전체 패키지의 결정적 검증 명령은 다음과 같다.

`cd gateway-go && go test ./internal/domain/wiki`

그래프·인덱스 변경은 검색 결과뿐 아니라 파일 재개방 후 결과도 검증한다.
Dreamer 변경은 적용 전 guard, 적용 결과, 중복 실행의 멱등성을 함께 확인한다.
