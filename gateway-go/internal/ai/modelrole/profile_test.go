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
