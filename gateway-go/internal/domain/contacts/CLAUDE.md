# contacts (기기 주소록 미러) 지도

기기 주소록을 게이트웨이에 미러링하는 평면 조회 테이블이다. 위키(큐레이션된
인물 지식)와 의도적으로 분리돼 있다: 여기는 "이 번호/주소가 누구지?"에 답하는
벌크 룩업이지 지식 베이스가 아니다.

## 진입점과 책임

- `store.go` — canonical `Contact` 타입, `Store`, `NewStore(path)`,
  전량 교체 동기화(ReplaceAll)·전화/이메일 룩업(LookupPhone/LookupEmail)·
  검색(Search)·핫워드 힌트.
- `person_name.go` — 주소록, 위키 인물 보강, 분류 규칙이 공유하는
  `NormalizePersonName` match-key 계약. 별도 소비 패키지에서 이름 정규화를
  복제하거나 wrapper export를 만들지 않는다.

## 의존 방향과 불변조건

- 동기화는 항상 **전량 교체 스냅숏**이다 — 부분 병합/증분 갱신을 추가하지
  않는다. 기기가 단일 진실원이고, 게이트웨이 사본은 언제든 다음 동기화로
  덮인다.
- 위키 인물 페이지로의 승격/조인은 소비자(회상·이메일 신원 조인) 소관이다 —
  이 스토어가 위키를 임포트하는 방향은 금지(계층 역전).
- 위키가 주소록 DTO와 이름 match key를 소비하는 방향은 허용하지만,
  `contacts.Contact`에 위키 전용 필드를 추가하지 않는다.
- 룩업 정규화(전화번호 자리·이메일 소문자화)는 저장 시가 아니라 조회 경로의
  계약이다 — 원본 문자열은 보존한다.

## 변경과 검증

`cd gateway-go && go test ./internal/domain/contacts`

룩업/검색 의미 변경은 `store_test.go`와 `store_contract_test.go`, 사람 이름
match-key 변경은 `person_name_test.go` 계약 단언을 함께 갱신한다.
