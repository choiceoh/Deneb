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

// evolveSystemPrompt instructs the LLM to improve an existing skill.
const evolveSystemPrompt = `당신은 AI 에이전트의 스킬 개선 시스템입니다.
기존 스킬의 사용 이력(성공/실패)을 분석하여 개선 제안을 생성합니다.

## 입력
- 현재 SKILL.md 내용
- 사용 이력: 성공 횟수, 실패 횟수, 실패 패턴
- 관련 피드백

## 개선 원칙
1. **최소 변경**: 작동하는 부분은 건드리지 않음
2. **실패 패턴 해결**: 반복되는 실패 원인을 직접 해결
3. **명확성 향상**: 모호한 지시를 구체화
4. **버전 업**: 변경 시 patch 버전 증가
5. **범주 수준 유지**: 특정 세션/PR/에러 문자열에 과적합하지 않음
6. **사용자 교정 반영**: 형식, 검증, 범위 제한에 대한 사용자 교정은 스킬 개선 신호로 취급
7. **Review finding 우선**: 입력에 "Review Finding"이 있으면, 이는 방금 끝난 세션을 관찰한 리뷰가 도출한 구체적 개선 지시다. 사용 이력이 0건이어도 데이터 부족을 이유로 skip하지 말고 finding을 신뢰해 직접 반영하라 (안전성은 이후 별도 검증 단계가 보장한다)

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
1. 개선본이 원본보다 명확하거나 최소한 동등하다 (정보·절차 퇴보 없음)
2. 존재하지 않는 도구·명령어·파일 경로를 지어내지 않았다
3. 핵심 구조를 유지한다 (When to Use / Procedure / Pitfalls / Verification 등)
4. 범주 수준을 유지한다 (특정 PR 번호·에러 문자열·세션에 과적합하지 않음)
5. 사용 이력의 실패 패턴을 실제로 해결하는 방향이다

## 출력 (JSON만)
{"pass": true, "reason": "간단한 근거"}
또는
{"pass": false, "reason": "거부 사유"}

확신이 없으면 pass=false. 잘못된 개선보다 원본 유지가 안전합니다.`
