package rlm

// SubAgentSystemPrompt returns the system prompt for sub-LLM agents.
// Empty string: sub-agents receive only the caller's prompt, matching
// the original RLM design where llm_query has no system prompt overhead.
func SubAgentSystemPrompt() string {
	return ""
}
