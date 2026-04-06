package rlm

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
