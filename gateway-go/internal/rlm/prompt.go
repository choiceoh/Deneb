package rlm

import "fmt"

// DataAccessPrinciples returns the system prompt addition for RLM mode.
// It instructs the LLM to use the REPL tool for conversation history exploration
// and data-on-demand access instead of relying on pre-injected context.
func DataAccessPrinciples() string {
	cfg := ConfigFromEnv()
	return fmt.Sprintf(`## REPL 환경

대화 기록은 repl 도구의 context 변수에 저장되어 있습니다.
프롬프트에는 최근 %d개 메시지만 포함됩니다. 이전 대화는 repl로 탐색하세요.

context 구조: [struct(seq=1, role="user", content="...", created_at=1712000000000), ...]

사용 가능한 함수:
- llm_query(prompt) → 서브 LLM 호출 (요약, 분석, Q&A)
- llm_query_batch(prompts) → 병렬 서브 LLM
- regex_search(pattern, text) → 정규식 매치 (첫 번째)
- regex_findall(pattern, text) → 모든 정규식 매치
- FINAL(answer) → 최종 답변
- FINAL_VAR(var_name) → 변수값을 최종 답변으로
- SHOW_VARS() → 현재 변수 목록

탐색 패턴:
  # 전체 길이 확인
  print(len(context))

  # 키워드 검색
  matches = [m for m in context if "키워드" in m.content]

  # 유저 메시지만 필터
  user_msgs = [m for m in context if m.role == "user"]

  # 시간대 필터 (epoch ms)
  recent = [m for m in context if m.created_at > 1712000000000]

  # 청크 분석 (서브 LLM 활용)
  chunk = str([m.content for m in context[100:200]])
  result = llm_query("이 대화에서 핵심 결정사항: " + chunk)

  # 정규식 패턴 매칭
  nums = regex_findall("[0-9]+", context[0].content)

주의:
- 단순 인사나 최근 대화 질문에는 repl 불필요 (프롬프트에 최근 대화 포함)
- "전에", "지난번", "아까" 같은 과거 참조 → repl로 context 검색
- 변수는 repl 호출 간 유지됨 (같은 요청 내)
- f-string 사용 가능: print(f"총 {len(context)}개")

## 데이터 접근 원칙

프로젝트 데이터에는 반드시 도구를 통해 접근하세요:
1. 과거 결정/인물/기술 지식 → wiki search
2. 프로젝트 데이터 → projects_list, projects_get_field, projects_search, projects_get_document
3. 과거 대화 기억 → repl로 context 검색 또는 wiki search
4. 단순 인사나 일반 질문에는 도구를 사용하지 말 것`, cfg.FreshTailCount)
}

// SubLLMPrinciples returns the system prompt addition for Phase 2 sub-LLM usage.
func SubLLMPrinciples() string {
	return `## 서브 LLM 활용

복수 프로젝트를 비교/분석해야 할 때, llm_spawn_batch로 병렬 처리하세요.

사용 시점:
- 3개 이상 프로젝트의 동일 항목을 비교할 때
- 각 프로젝트에 대해 동일한 분석을 반복해야 할 때
- 대량 데이터를 요약한 뒤 종합해야 할 때

절차:
1. projects_list로 대상 프로젝트 목록 확인
2. llm_spawn_batch로 각 프로젝트별 분석 태스크를 병렬 실행
3. 결과를 종합하여 비교/분석 답변 작성

주의:
- 단일 프로젝트 조회에는 llm_spawn 대신 직접 도구 호출이 효율적
- 서브 LLM에게는 구체적이고 명확한 태스크를 지시할 것`
}

// SubAgentSystemPrompt returns the compact system prompt for sub-LLM agents.
// Sub-agents get a focused prompt optimized for data retrieval and summarization.
func SubAgentSystemPrompt() string {
	return `당신은 데이터 분석 보조입니다. 주어진 질문에 대해 도구를 사용하여 정확하고 간결하게 답변하세요.

규칙:
- 핵심 수치와 사실만 포함
- 200자 이내로 답변
- 추측하지 말 것. 도구로 확인한 사실만 기술
- 한국어로 답변`
}
