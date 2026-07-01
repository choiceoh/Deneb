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
- PR 번호, 에러 문자열, 코드명, 세션 전용 이름처럼 특정 1회성 아티팩트에 묶인 패턴
- "사용자가 누구인가/무엇을 좋아하는가" 같은 장기기억 사실
- 도구/환경에 대한 부정 단정 ("exec는 안 됨", "X 도구는 고장났다", "이 API는 못 쓴다"). 미설정·자격증명·일시적 오류 같은 환경 의존 실패를 스킬로 굳히면, 미래에 그 도구를 아예 회피하는 거부(refusal)로 하드닝되어 능력이 퇴행한다. 실패는 절차 개선(전제 확인·재시도·대체 경로)으로만 담고, "안 된다"는 결론 자체는 스킬화하지 마라.

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
> Deneb는 단일 사용자 비서다. 스킬은 "모든 사용자에게 일반화"될 필요가 없고, 이 사용자의 반복 업무(특정 거래처·문서 유형·프로젝트·리포트 처리)에 다시 쓰이면 충분하다. 즉 "좁다"는 이유만으로 skip 하지 마라 — 절차가 구체적이고 다시 나올 업무면 만들어라. 모호하거나(위 기준) 1회성 아티팩트에 묶였을 때만 skip 한다.

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
// heuristic can't — semantic duplicates and vague/one-off skills.
// Self-generated skills accrue debt if they pile up unvalidated (SoK -1.3pp),
// but Deneb is single-user with an active curator that prunes unused skills, so
// the judge blocks only clear pollution (dup / vague / single-artifact one-off)
// and admits a borderline-but-reusable domain skill as probationary rather than
// rejecting on doubt. A narrow skill that recurs in THIS user's work is valuable.
const genesisJudgeSystemPrompt = `당신은 AI 에이전트 스킬 라이브러리의 품질 게이트키퍼입니다.
새로 생성된 스킬 후보가 라이브러리에 추가할 가치가 있는지 판정합니다.
검증 없이 쌓이는 자기생성 스킬은 부채가 되지만, 이 시스템은 단일 사용자용이고 미사용 스킬을 정리하는 큐레이터가 있습니다. 따라서 명백한 오염(중복·모호·1회성)만 막고, 이 사용자의 반복 업무에 유용한 구체적 스킬은 경계선이라도 통과시킵니다.

## 거부(pass=false) 기준 — 하나라도 해당하면 거부
1. **중복**: 기존 스킬 중 하나와 실질적으로 같은 일을 한다 (도구 조합·트리거·목적이 겹침). 이름이 달라도 "무엇을 언제 하는가"가 비슷하면 중복이다.
2. **모호/비실행**: 구체적 절차·도구 호출·명령어 없이 일반론만 있다 ("맥락을 잘 살펴라").
3. **1회성**: 특정 세션·PR·에러 문자열·1회성 작업에 묶여 다시 나오지 않는다. (주의: "좁다/도메인 특정적"이라는 이유만으로 거부하지 마라 — 단일 사용자 비서에서는 특정 거래처·문서·프로젝트에 대한 반복 절차가 좁아도 재사용 가치가 있다. 거부는 "단일 아티팩트에 묶여 재발 안 함"일 때만.)
4. **저가치**: 단순 조회/Q&A 수준이라 별도 스킬로 둘 이득이 없다.

## 통과(pass=true) 기준
- 기존 스킬과 중복이 아니고, 이 사용자의 업무에 다시 쓰일 구체적 절차가 있다 (범용 일반화는 요구하지 않는다).

## 출력 (JSON만)
{"pass": true, "reason": "간단한 근거"}
또는
{"pass": false, "reason": "거부 사유 (중복이면 어떤 기존 스킬과 겹치는지 명시)"}

명백히 위 거부 기준에 해당할 때만 pass=false. 애매하면 pass=true로 통과시키되 저확신임을 reason에 남겨라 — 미사용·저품질 스킬은 큐레이터가 이후 정리하므로, 경계선 후보를 놓치는 것이 더 큰 손실이다.`

// evolveSystemPrompt instructs the LLM to improve an existing skill.
const evolveSystemPrompt = `당신은 AI 에이전트의 스킬 개선 시스템입니다.
기존 스킬의 사용 이력(성공/실패)을 분석하여 개선 제안을 생성합니다.

## 입력
- 현재 SKILL.md 내용
- 사용 이력: 성공 횟수, 실패 횟수, 실패 패턴
- Self-Harness failure evidence bundle이 있으면, 최근 실제 실패를 terminal cause / causal status / reusable agent mechanism으로 묶은 근거로 취급할 것
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
11. **Self-Harness targeting**: 후보는 지원(support)이 있는 failure evidence bundle의 한 가지 주 실패 메커니즘 또는 명시적인 Review Finding에 연결되어야 한다. evidence가 약하거나 스킬 본문으로 addressable하지 않으면 억지로 고치지 말고 skip하라
12. **Auditability**: changes.description에는 목표 failure signature, 수정한 editable surface(예: Procedure/Pitfalls/Verification), 기대 행동 변화, 회귀 위험을 한 문장으로 포함하라
13. **Promotion gate**: skip=false라면 target_signature, edited_surface, expected_behavior_change, regression_risk는 필수다. failure evidence bundle을 쓰는 경우 target_signature는 bundle의 signature 문자열과 일치해야 한다. edited_surface는 실제로 바꾼 SKILL.md body section이어야 하며, 이 evolve 경로에서 수정할 수 없는 support-file/runtime 표면을 주장하지 마라
14. **Hermes-style patch first**: 출력 형식은 전체 body지만, 실제 변경은 작은 targeted patch여야 한다. 스킬 제목/목적을 바꾸거나 여러 섹션을 동시에 크게 갈아엎지 마라. 그런 변경이 필요하면 skip하고 source-level self-correction 또는 별도 review 후보로 남겨야 한다
15. **Size/cache guard**: 후보는 frontmatter 포함 15KB 이하로 유지하고, 대화 중 시스템 프롬프트·도구셋·외부 support file 변경을 요구하지 마라. 이 evolve 경로는 SKILL.md body만 바꾼다

## 출력 형식

개선이 필요할 때:
{
  "skip": false,
  "changes": {
    "description": "변경 요약",
    "new_version": "0.1.1",
    "target_signature": "failure evidence bundle의 목표 signature 또는 review finding 요약",
    "edited_surface": "Procedure|Pitfalls|Verification|metadata|support-file 중 실제 수정한 표면",
    "expected_behavior_change": "후보가 유도할 관찰 가능한 행동 변화",
    "regression_risk": "회귀 위험과 보존해야 할 동작",
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
- body는 원본 제목과 목적을 유지한 patch-sized 본문이어야 합니다. 제목/목적 변경, 15KB 초과, 광범위 rewrite가 필요하면 skip=true로 거절하세요.
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
7. Self-Harness failure evidence bundle이 입력에 있으면 후보가 지원 있는 주 실패 메커니즘을 겨냥하거나, addressable하지 않은 경우 불필요한 변경을 피한다
8. Hermes-style promotion gate를 지킨다: patch-sized 변경, 원본 제목/목적 보존, frontmatter 포함 15KB 이하, SKILL.md body 외부의 support file/runtime/toolset 변경 요구 없음
9. original_score와 candidate_score를 0~100 정수/실수로 매기고, candidate_score가 original_score보다 최소 3점 높을 때만 pass=true다

## 출력 (JSON만)
{"pass": true, "original_score": 72, "candidate_score": 80, "reason": "간단한 근거"}
또는
{"pass": false, "original_score": 72, "candidate_score": 72, "reason": "거부 사유"}

확신이 없거나 점수 차이가 3점 미만이면 pass=false. 잘못된 개선보다 원본 유지가 안전합니다.`
