package modelrole

import "strings"

// IsReasoningModel reports whether model denotes a reasoning model — one that
// generates a separate chain-of-thought channel, surfaced as reasoning_content
// on OpenAI-compatible (vLLM) streaming responses.
//
// Detection is name-based. vLLM serves these behind --reasoning-parser, which
// emits the thinking channel regardless of chat_template_kwargs.enable_thinking,
// so callers cannot assume that flag suppressed it. The explicit non-thinking
// Qwen3 *-instruct-* variants are excluded.
func IsReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case m == "":
		return false
	case strings.Contains(m, "qwq"):
		return true
	case strings.Contains(m, "deepseek-r1"), strings.Contains(m, "deepseek-reasoner"):
		return true
	case strings.Contains(m, "gpt-oss"):
		return true
	case strings.Contains(m, "qwen3"):
		// Qwen3 thinking and hybrid variants emit reasoning_content; only the
		// explicit *-instruct-* variants ship with thinking disabled.
		return !strings.Contains(m, "instruct")
	default:
		return false
	}
}

// RoleIsReasoning reports whether the model configured for role is a reasoning
// model. An unconfigured role (empty model name) is not a reasoning model.
func (r *Registry) RoleIsReasoning(role Role) bool {
	return IsReasoningModel(r.Model(role))
}
