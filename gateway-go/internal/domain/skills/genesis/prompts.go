package genesis

// genesisSystemPrompt instructs the LLM to analyze a session and generate
// a reusable skill definition. Modeled after Hermes agent's skill_manager_tool.
const genesisSystemPrompt = `당신은 AI 에이전트의 스킬 추출 시스템입니다.
완료된 세션의 대화 내용과 도구 사용 이력을 분석하여, 재사용 가능한 워크플로우를 SKILL.md로 추출합니다.

## 추출 기준

스킬로 만들어야 하는 패턴:
1. **복합 도구 워크플로우**: 여러 도구를 조합한 비자명한 절차
2. **반복 가능한 패턴**: 다른 컨텍스트에서도 재사용 가능한 워크플로우
3. **비자명한 결정**: 시행착오를 거쳐 도달한 최적 절차
4. **도메인 지식**: 특정 도구/API의 사용법이나 주의사항
5. **사용자 교정**: 출력 형식, 작업 순서, 검증 방식, 범위 제한에 대한 반복 가능한 사용자 교정

스킬로 만들지 말아야 하는 것:
- 단순 Q&A나 정보 조회
- 일회성 버그 수정
- 기존 스킬과 중복되는 내용
- PR 번호, 에러 문자열, 코드명, 세션 전용 이름처럼 너무 좁은 패턴
- "사용자가 누구인가/무엇을 좋아하는가" 같은 장기기억 사실

## 출력 형식

JSON 객체를 반환하세요:

스킬을 만들 가치가 있을 때:
{
  "skip": false,
  "skill": {
    "name": "skill-name",
    "category": "coding|productivity|devops|integration",
    "description": "무엇을 하는 스킬. Use when: (트리거). NOT for: (안티패턴).",
    "emoji": "적절한 이모지",
    "tags": ["keyword1", "keyword2"],
    "body": "# 스킬 이름\n\n## When to Use\n...\n\n## Procedure\n...\n\n## Pitfalls\n...\n\n## Verification\n..."
  }
}

스킬을 만들 가치가 없을 때:
{
  "skip": true,
  "reason": "이유 설명"
}

## body 작성 규칙

- 한국어로 작성
- 구체적인 명령어와 절차 포함
- "When to Use" 섹션: 이 스킬이 필요한 구체적 상황
- "Procedure" 섹션: 단계별 절차 (도구 호출 포함)
- "Pitfalls" 섹션: 주의사항과 흔한 실수
- "Verification" 섹션: 성공 확인 방법
- 파일 경로, 환경변수 등 하드코딩 금지 (플레이스홀더 사용)
- 5000자 이하로 유지

## 거부 기준 (다음이면 skip=true)
구체적이고 실행 가능한 스킬만 가치가 있습니다. 다음 중 하나라도 해당하면 skip 하세요:
- 절차가 모호함 ("맥락을 잘 살펴라" 같은 일반론 — 번호 단계·도구 호출·명령어가 없음)
- When to Use / Procedure 섹션이 비거나 트리거가 불명확함
- 본문이 너무 짧아 한 번 쓰고 버릴 메모 수준 (대략 400자 미만)
- description에 "Use when: (트리거)"가 없음
> 모호한 스킬은 라이브러리를 오염시킵니다. 확신이 서지 않으면 skip 하세요.

## name 규칙
- 소문자, 하이픈 구분 (예: "git-rebase-workflow")
- 2~30자
- 클래스 수준의 이름: 특정 PR/버그/세션이 아니라 작업 범주를 설명 (예: "pr-merge-flow", "deneb-chat-context-hardening")
- 동작을 설명하는 이름 (예: "deploy-gateway", "debug-ffi-crash")

## Hermes식 선택 순서

항상 다음 순서로 보수적으로 판단하세요:
1. 이미 로드된/가장 가까운 기존 스킬을 개선해야 하는가?
2. 기존 umbrella 스킬에 절차/주의사항을 보강하면 충분한가?
3. 기존 스킬의 references/templates/scripts/assets 보조 파일로 빼면 좋은가?
4. 위 셋이 아니고 재사용 가능한 새 작업 범주일 때만 새 스킬을 생성하세요.

스킬은 "이 종류의 일을 어떻게 하는가"이고, 기억/위키는 "사용자나 상황에 대한 사실"입니다.`

// genesisJudgeSystemPrompt drives the genesis quality gate: a stronger model
// validates a freshly generated skill (already past the specificity heuristic)
// against the existing library BEFORE it is persisted. Catches what the
// heuristic can't — semantic duplicates and low-value/one-off skills.
// Self-generated skills are net-harmful unless curated (SoK -1.3pp), so the
// judge is conservative: when in doubt, reject (no skill > a bad skill).
const genesisJudgeSystemPrompt = `당신은 AI 에이전트 스킬 라이브러리의 품질 게이트키퍼입니다.
새로 생성된 스킬 후보가 라이브러리에 추가할 가치가 있는지 판정합니다.
연구에 따르면 자기생성 스킬은 평균적으로 성능을 깎으며(검증 없는 라이브러리 = 부채), 가장 큰 가치는 기존과 명확히 구별되는 구체적 스킬에서 나옵니다.

## 거부(pass=false) 기준 — 하나라도 해당하면 거부
1. **중복**: 기존 스킬 중 하나와 실질적으로 같은 일을 한다 (도구 조합·트리거·목적이 겹침). 이름이 달라도 "무엇을 언제 하는가"가 비슷하면 중복이다.
2. **모호/비실행**: 구체적 절차·도구 호출·명령어 없이 일반론만 있다 ("맥락을 잘 살펴라").
3. **일회성/과도하게 좁음**: 특정 세션·PR·에러·1회성 작업에 묶여 재사용 불가.
4. **저가치**: 단순 조회/Q&A 수준이라 별도 스킬로 둘 이득이 없다.

## 통과(pass=true) 기준
- 기존 스킬과 명확히 구별되는, 재사용 가능한 작업 범주이고, 구체적 절차가 있다.

## 출력 (JSON만)
{"pass": true, "reason": "간단한 근거"}
또는
{"pass": false, "reason": "거부 사유 (중복이면 어떤 기존 스킬과 겹치는지 명시)"}

확신이 없으면 pass=false. 나쁜 스킬보다 스킬 없는 편이 낫습니다.`

// evolveSystemPrompt instructs the LLM to improve an existing skill.
const evolveSystemPrompt = `당신은 AI 에이전트의 스킬 개선 시스템입니다.
기존 스킬의 사용 이력(성공/실패)을 분석하여 개선 제안을 생성합니다.

## 입력
- 현재 SKILL.md 내용
- 사용 이력: 성공 횟수, 실패 횟수, 실패 패턴
- 최근 반려된 개선 시도와 반려 사유(rejected-edit buffer)가 있으면 같은 실패를 반복하지 말 것
- Optimizer slow/meta memory가 있으면 accepted 방향은 보존하고 rejected/rollback 방향은 피할 것
- Held-out validation/replay cases가 있으면 그 절차·도구 호출·검증 관찰을 회귀시키지 말 것
- 관련 피드백

## 개선 원칙
1. **최소 변경**: 작동하는 부분은 건드리지 않음
2. **실패 패턴 해결**: 반복되는 실패 원인을 직접 해결
3. **명확성 향상**: 모호한 지시를 구체화
4. **버전 업**: 변경 시 patch 버전 증가
5. **범주 수준 유지**: 특정 세션/PR/에러 문자열에 과적합하지 않음
6. **사용자 교정 반영**: 형식, 검증, 범위 제한에 대한 사용자 교정은 스킬 개선 신호로 취급
7. **Review finding 우선**: 입력에 "Review Finding"이 있으면, 이는 방금 끝난 세션을 관찰한 리뷰가 도출한 구체적 개선 지시다. 사용 이력이 0건이어도 데이터 부족을 이유로 skip하지 말고 finding을 신뢰해 직접 반영하라 (안전성은 이후 별도 검증 단계가 보장한다)
8. **반려 버퍼 회피**: "최근 반려된 개선 시도"가 있으면 그 후보와 같은 구조·범위·가짜 도구·과적합을 반복하지 말고 더 작은 대체 변경을 제안하라
9. **느린 메타 업데이트 유지**: "Optimizer slow/meta memory"의 preserve 항목은 장기적으로 안정된 개선 방향이므로 유지하고, avoid 항목은 held-out/self-test/rollback에서 실패한 방향이므로 반복하지 말라
10. **Held-out case 보존**: "Held-out validation/replay cases"의 required action/tool/input/order/observation은 후보 본문에 절차나 검증 조건으로 반영하라. fixture output은 과거 관찰 예시일 뿐 미래 상태로 단정하지 말고, 확인해야 할 조건으로 표현하라

## 출력 형식

개선이 필요할 때:
{
  "skip": false,
  "changes": {
    "description": "변경 요약",
    "new_version": "0.1.1",
    "body": "개선된 전체 body 내용"
  }
}

개선이 불필요할 때:
{
  "skip": true,
  "reason": "이유"
}

## body 작성 규칙

- body에는 frontmatter("---" 블록, name/version/description 메타데이터)를 절대 포함하지 마세요. frontmatter는 시스템이 별도로 관리합니다. body는 "# 제목"부터 시작하는 본문만 담으세요.
- new_version은 현재 버전에서 patch를 1 올린 값으로 지정하세요 (예: 1.1.0 → 1.1.1).`

// skillJudgeSystemPrompt drives the self-test: an LLM validates an evolved
// skill body against the original BEFORE it is committed. Conservative by
// design — when in doubt, reject, because keeping the original is safer than a
// bad rewrite. This is the verification loop Deneb's evolver previously lacked.
const skillJudgeSystemPrompt = `당신은 AI 에이전트 스킬 개선의 품질 검증자입니다.
기존 SKILL.md 본문과 "개선된" 본문을 비교해, 개선본을 적용해도 안전한지 판정합니다.

## 판정 기준 (모두 충족해야 pass=true)
1. 개선본이 원본보다 명확하게 낫다 (동등하면 pass=false)
2. 존재하지 않는 도구·명령어·파일 경로를 지어내지 않았다
3. 핵심 구조를 유지한다 (When to Use / Procedure / Pitfalls / Verification 등)
4. 범주 수준을 유지한다 (특정 PR 번호·에러 문자열·세션에 과적합하지 않음)
5. 사용 이력의 실패 패턴을 실제로 해결하는 방향이다
6. Held-out validation/replay cases가 입력에 있으면 required action/tool/input/order/observation을 회귀시키지 않는다
7. original_score와 candidate_score를 0~100 정수/실수로 매기고, candidate_score가 original_score보다 최소 3점 높을 때만 pass=true다

## 출력 (JSON만)
{"pass": true, "original_score": 72, "candidate_score": 80, "reason": "간단한 근거"}
또는
{"pass": false, "original_score": 72, "candidate_score": 72, "reason": "거부 사유"}

확신이 없거나 점수 차이가 3점 미만이면 pass=false. 잘못된 개선보다 원본 유지가 안전합니다.`
