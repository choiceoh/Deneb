package modelrole

import "strings"

// Profile holds model-specific tuning the gateway applies whenever a given model
// is selected: whether the model emits a reasoning channel, and vendor-
// recommended sampling defaults. It is the single source of truth so reasoning
// detection (IsReasoningModel) and the localai hub's sampling defaults stay
// consistent — adding a model in one place updates every caller.
type Profile struct {
	// Reasoning is true when the model emits a separate chain-of-thought channel
	// (served behind vLLM --reasoning-parser, surfaced as reasoning_content on
	// streams). These models emit the channel regardless of
	// chat_template_kwargs.enable_thinking, so callers must NOT attach that flag
	// — a thinking-only chat template 400s on the unknown kwarg.
	Reasoning bool

	// Temperature, TopP, TopK are vendor-recommended sampling defaults. A nil
	// pointer means "no override — use the server default".
	Temperature *float64
	TopP        *float64
	TopK        *int
}

// ProfileFor returns the tuning profile for a model name. Detection is
// case-insensitive substring matching. An unknown or empty model gets the zero
// Profile (non-reasoning, no sampling overrides).
func ProfileFor(model string) Profile {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case m == "":
		return Profile{}

	case strings.Contains(m, "step3") || strings.Contains(m, "step-3"):
		// Step-3.7: an MTP reasoning model whose chat template force-opens a
		// <think> block every turn. It always emits a reasoning channel and
		// cannot truly disable thinking (see llm/openai.go's reasoning_effort
		// floor). No vendor sampling override is published, so the server default
		// is used. The key effect of this entry is Reasoning=true: without it
		// step3p7 was misclassified as non-reasoning, which attached
		// enable_thinking=false (400 risk on its thinking-only template) and
		// mis-ordered the hub fallback chain.
		return Profile{Reasoning: true}

	case strings.Contains(m, "qwen3") || strings.Contains(m, "qwen36") || strings.Contains(m, "qwen35"):
		// Qwen3 family: recommended sampling temp 0.7 / top_p 0.8 / top_k 20
		// (qwen.readthedocs.io). The explicit *-instruct-* variants ship with
		// thinking disabled; all others emit the reasoning channel.
		return Profile{
			Reasoning:   !strings.Contains(m, "instruct"),
			Temperature: ptr(0.7),
			TopP:        ptr(0.8),
			TopK:        ptr(20),
		}

	case strings.Contains(m, "qwq"),
		strings.Contains(m, "deepseek-r1"), strings.Contains(m, "deepseek-reasoner"),
		strings.Contains(m, "gpt-oss"):
		return Profile{Reasoning: true}

	default:
		return Profile{}
	}
}

func ptr[T any](v T) *T { return &v }
