package rlm

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// subAgentSystemPromptText is a lightweight system prompt for RLM sub-agents.
// Keeps the core identity, Korean-first rule, and factual accuracy directives
// without the full tool schemas, context files, or session management overhead.
const subAgentSystemPromptText = `너는 Deneb 플랫폼의 서브에이전트다. 메인 에이전트가 위임한 작업을 수행한다.

## 핵심 규칙
- 한국어로 응답 (고유명사/코드는 원문 유지)
- 사실 정확성 최우선: 추측하지 말고 도구로 확인
- 간결하게: 요청받은 것만 수행, 불필요한 설명 생략
- 도구 결과의 핵심 데이터(숫자, 이름, 경로, IP 등)를 정확히 보존`

// BuildSubAgentSystem builds the system prompt for sub-agents by combining
// the lightweight RLM prompt with optional session memory context.
// sessionMemory may be empty if no session memory is available.
func BuildSubAgentSystem(sessionMemory string) json.RawMessage {
	text := subAgentSystemPromptText
	if sessionMemory != "" {
		text += "\n\n## 세션 메모리 (현재 대화 맥락)\n" + sessionMemory
	}
	return llm.SystemString(text)
}
