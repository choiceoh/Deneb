# 0005. 정본 사실의 권위는 호출자가 아니라 쓰기 경로가 정한다

**Status:** accepted

## Context

[ADR-0004](0004-canonical-fact-plane.md)의 정본 평면은 "누가 주장했는가"로 승자를
가른다. 그래서 **권위를 누가 발급하느냐**가 평면 전체의 신뢰를 결정한다.

PR #4653은 모델이 부르는 `knowledge(op="assert_fact"|"forget_fact")`에 `authority`
문자열을 받아서 모델이 `primary_document`를 자칭할 수 있었다. 근거 문자열은 문서가
실재한다는 증거도, 그 문서가 그 값을 뒷받침한다는 증거도 아니다. #4659는 이 위험을
정확히 짚었지만 **op 자체를 삭제**했고, 그 결과:

- 정본에 쓰는 경로가 정규식 유도 하나만 남아, 에이전트는 문서에서 확인한 사실조차
  기록할 수 없었다.
- `knowledge.Router.RecordFact`/`ForgetFact`와 wiki 어댑터의 fact 쓰기 계층이
  비테스트 호출자 0인 데드코드로 남았다.
- 금액·기한·계약을 위해 존재하는 `primary_document` 정책 표면이 도달 불가능해졌다.

즉 문제는 "모델이 쓰기"가 아니라 "**모델이 권위를 자칭하기**"였는데, 삭제는 전자를
막았다.

## Decision

두 op을 복원하되, **권위는 경로가 발급한다**.

- 모델 도구 스키마에 `authority`·`basis_at` **필드를 두지 않는다**. 문자열 검사에
  의존하지 않고 JSON 경계에서 애초에 받을 수 없게 한다.
- `knowledge` 어댑터는 fact 쓰기를 `agent_confirmed`(또는 명시적 `inference`)로
  상한한다. `direct_user`는 인증된 네이티브 직접 발화 induction 전용,
  `primary_document`/`runtime_observation`은 자기 출처를 인증하는 내부 ingestion
  전용이며, 둘 다 wiki `Store`를 직접 호출한다.
- `assert_fact`는 **`source_refs` 필수**다. 어디서 왔는지 대지 못하는 에이전트 주장은
  정본 평면이 막으려는 바로 그 종류의 주장이다.
- 에이전트 tombstone은 더 높은 권위의 현행 사실을 은퇴시키지 못한다. 시도는
  `ignored_lower_authority`로 이력에만 남고 사용자 사실은 유지된다.
- promptware로 오염된 턴에서는 두 mutation op이 비가역 도구 게이트에 걸려 실행되지
  않는다 (`exec`·`preference`·`wiki_forget`과 같은 등급). `knowledge`의 나머지 op은
  계속 열려 있어, 오염된 턴도 조회하고 상황을 설명할 수 있다.
- **오염 게이트를 켜지 않는 합성 경로는 자체 게이트가 모든 쓰기 op을 막아야 한다.**
  메일 분석 합성(`mailAnalysisAgentToolGate`)이 그 예다 — 외부가 작성한 본문을 읽으면서
  `GateUntrustedTools`를 켜지 않으므로, 그 게이트가 유일한 방어선이다. op 판별은
  도구 자신의 별칭 규칙(`recallops.KnowledgeOpFromInput`)을 공유해야 한다: `action`,
  `write`, `쓰기` 같은 별칭을 못 보는 게이트는 막으려던 호출을 그대로 통과시킨다.

## Alternatives rejected

- **#4659 상태 유지(삭제)** — 도달 불가능한 정책 표면과 데드코드가 남고, 에이전트가
  근거를 확인하고도 기록할 수 없다. 정본 쓰기 입구가 정규식 문법 하나로 좁아진다.
- **`authority`를 받되 값만 거부** — 어댑터의 문자열 검사는 이미 있었지만, 스키마에
  필드가 보이는 한 모델은 계속 시도하고 방어는 한 겹뿐이다. 필드를 없애면 그 시도가
  성립하지 않는다.
- **모델 쓰기를 승인 큐로 보내기** — 단일 사용자 에이전트에서 승인 큐는 사용자에게
  일거리를 넘길 뿐이다. `agent_confirmed`가 사용자 사실을 절대 못 이긴다는 순위
  규칙이 같은 보호를 자동으로 준다.

## Consequences

- `primary_document`/`runtime_observation`은 **여전히 도달 불가능**하다. 그 권위를
  쓰려면 출처를 인증하는 ingestion 경로(예: 메일·문서 파이프라인)를 만들어 `Store`를
  직접 호출해야 하며, 그때 이 ADR을 supersede하지 말고 그 경로를 추가하면 된다.
- **"인용한 페이지를 열어보고 승격"은 그 경로가 아니다** (검토된 뒤 철회 —
  `wiki/fact_source.go` 머리말). 이 위키에는 **페이지 단위 provenance가 없다**:
  같은 모델이 `knowledge(op="record")`로 아무 페이지나 쓸 수 있고 어댑터가 거기에
  오늘 날짜를 찍는다. 그래서 "본문에 값이 있고 날짜가 있는 페이지"는 모델이 방금
  만들어낼 수 있는 조건이고, `primary_document`는 amount/deadline/contract에서
  `direct_user`보다 **위**라 결과적으로 모델 주장이 사용자 발화를 덮게 된다 — 이 ADR이
  막으려던 세탁이 다른 문으로 돌아온다. 덧붙여 본문에 문자열이 있다는 것은 그 페이지가
  그 값을 **현행으로 주장한다**는 뜻이 아니다(기각된 안·이력 줄·생성된 projection이
  모두 같게 읽힌다). 따라서 승격의 **선결 조건은 페이지 provenance** — 인증된 ingestion이
  쓴 페이지와 모델이 쓴 페이지를 저장 계층에서 구분하는 것 — 이며, 그전까지 승격은 없다.
- `source_refs`는 그래도 서버가 열어본다(`Store.VerifyFactSource`). 다만 결과는 **권위가
  아니라 보고**다: ref가 열리지 않거나 그 페이지가(본문이든 frontmatter든) 주장 값을 담고
  있지 않으면 도구 응답이 그렇게 말해준다. 위키가 아닌 계층 ref(`f:…`)는 이 저장소가
  판단할 수 없으므로 **미검증으로 남기고 지적하지 않는다** — 멀쩡한 인용을 고치라고
  시키지 않기 위해서다. 기록되는 권위는 어느 쪽이든 `agent_confirmed` 그대로다.
- 도구로 기록된 사실은 사용자 정정에 항상 진다. 사용자가 직접 말한 값이 있으면
  에이전트 주장은 `superseded`로 이력에만 남는다.
- 근거 없는 주장은 도구 오류로 되돌아온다. 모델은 먼저 `knowledge(op="recall")`로
  ref를 확보해야 한다.
