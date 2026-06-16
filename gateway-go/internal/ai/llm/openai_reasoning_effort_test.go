package llm

import "testing"

// dsv4 (DeepSeek-V4 family) only accepts reasoning_effort "high"/"max"; "low"/
// "medium" are rejected. applySamplingParams must clamp dsv4 to "high" regardless
// of the thinking budget, while other models keep the budget→effort mapping.
func TestApplySamplingParams_ReasoningEffortFloor(t *testing.T) {
	cases := []struct {
		name   string
		model  string
		budget int
		want   string
	}{
		{"dsv4 small budget → high (not low)", "deepseek-v4-flash", 1024, "high"},
		{"dsv4 medium budget → high (not medium)", "deepseek-v4-flash", 10240, "high"},
		{"dsv4 large budget → high", "deepseek-v4-flash", 32768, "high"},
		{"dsv4 underscore variant → high", "deepseek_v4", 4096, "high"},
		{"non-dsv4 small budget → low", "qwen3.6-35b-a3b", 1024, "low"},
		{"non-dsv4 medium budget → medium", "qwen3.6-35b-a3b", 10240, "medium"},
		{"non-dsv4 large budget → high", "qwen3.6-35b-a3b", 32768, "high"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			oaiReq := &openAIRequest{}
			req := &ChatRequest{
				Model:    c.model,
				Thinking: &ThinkingConfig{Type: "enabled", BudgetTokens: c.budget},
			}
			applySamplingParams(oaiReq, req)
			if oaiReq.ReasoningEffort != c.want {
				t.Errorf("ReasoningEffort = %q, want %q", oaiReq.ReasoningEffort, c.want)
			}
		})
	}
}
