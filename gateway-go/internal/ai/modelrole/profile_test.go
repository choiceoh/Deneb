package modelrole

import "testing"

func TestProfileFor(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		reasoning   bool
		hasSampling bool // Temperature non-nil
	}{
		{"step3p7 bare", "step3p7", true, false},
		{"step3p7 provider-prefixed", "vllm/step3p7", true, false},
		{"step-3.7 dashed", "step-3.7", true, false},
		{"qwen3 thinking: reasoning + sampling", "qwen3-30b", true, true},
		{"qwen3 instruct: non-reasoning + sampling", "qwen3-30b-instruct", false, true},
		{"qwen36 alias", "qwen36", true, true},
		{"qwq: reasoning, no sampling", "qwq-32b", true, false},
		{"deepseek-v4: sampling, reasoning stays false", "deepseek-v4-flash", false, true},
		{"deepseek-v4 provider-prefixed", "vllm/deepseek-v4-flash", false, true},
		{"deepseek-r1: reasoning", "deepseek-r1", true, false},
		{"gpt-oss: reasoning", "gpt-oss-120b", true, false},
		{"gemma: non-reasoning, no sampling", "gemma4", false, false},
		{"empty", "", false, false},
		{"unknown", "llama-3-70b", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProfileFor(tt.model)
			if p.Reasoning != tt.reasoning {
				t.Errorf("ProfileFor(%q).Reasoning = %v, want %v", tt.model, p.Reasoning, tt.reasoning)
			}
			if (p.Temperature != nil) != tt.hasSampling {
				t.Errorf("ProfileFor(%q) hasSampling = %v, want %v", tt.model, p.Temperature != nil, tt.hasSampling)
			}
		})
	}
}

// TestIsReasoningModel_Step3p7 locks the core fix: step3p7 was previously
// misclassified (it matched no name in the old switch) and so got
// enable_thinking=false attached (400 risk) and was mis-ordered in the hub
// fallback chain. It must now be detected as a reasoning model.
func TestIsReasoningModel_Step3p7(t *testing.T) {
	for _, m := range []string{"step3p7", "vllm/step3p7", "step-3.7"} {
		if !IsReasoningModel(m) {
			t.Errorf("IsReasoningModel(%q) = false, want true", m)
		}
	}
}

// TestProfileFor_DeepseekV4Sampling pins the recommended values for the
// self-hosted main model: the shipped generation_config.json is 1.0/1.0,
// so dropping this profile silently reverts dsv4 to no-op sampling.
func TestProfileFor_DeepseekV4Sampling(t *testing.T) {
	p := ProfileFor("deepseek-v4-flash")
	if p.Temperature == nil || *p.Temperature != 0.6 {
		t.Errorf("deepseek-v4 Temperature = %v, want 0.6", p.Temperature)
	}
	if p.TopP == nil || *p.TopP != 0.95 {
		t.Errorf("deepseek-v4 TopP = %v, want 0.95", p.TopP)
	}
	if p.Reasoning {
		t.Error("deepseek-v4 Reasoning = true, want false (thinking is toggled via chat_template_kwargs, not the reasoning channel flag)")
	}
}

// TestProfileFor_Qwen3Sampling pins the vendor-recommended values so a future
// edit can't silently drop them.
func TestProfileFor_Qwen3Sampling(t *testing.T) {
	p := ProfileFor("qwen3-30b")
	if p.Temperature == nil || *p.Temperature != 0.7 {
		t.Errorf("qwen3 Temperature = %v, want 0.7", p.Temperature)
	}
	if p.TopP == nil || *p.TopP != 0.8 {
		t.Errorf("qwen3 TopP = %v, want 0.8", p.TopP)
	}
	if p.TopK == nil || *p.TopK != 20 {
		t.Errorf("qwen3 TopK = %v, want 20", p.TopK)
	}
}
