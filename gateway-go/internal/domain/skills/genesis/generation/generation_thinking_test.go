package generation

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestGenerationThinkingAppliesPerResolvedModel: the generation model is
// resolved per call (a session can pin its own), so the directive must be
// looked up by the model actually being called, not fixed at construction.
func TestGenerationThinkingAppliesPerResolvedModel(t *testing.T) {
	kwargs := map[string]string{"glm-5.3": "thinking", "qwen3.6-35b-a3b": "enable_thinking"}
	s := &Service{}
	s.SetGenerationThinking(func(model string) *llm.ThinkingConfig {
		return &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: kwargs[model]}
	})

	got := s.thinkingFor("glm-5.3")
	if got == nil || got.Type != "disabled" || got.TemplateKwarg != "thinking" {
		t.Fatalf("thinkingFor(glm-5.3) = %+v, want disabled/thinking", got)
	}
	if got := s.thinkingFor("qwen3.6-35b-a3b"); got == nil || got.TemplateKwarg != "enable_thinking" {
		t.Errorf("thinkingFor(qwen) = %+v, want the qwen kwarg — one model's toggle must not be sent to another", got)
	}
	// An unknown model still gets "disabled" with no kwarg: the directive is
	// harmless to a single-mode model and correct for a dual-mode one whose
	// capability entry has not been filled in.
	if got := s.thinkingFor("unknown-model"); got == nil || got.TemplateKwarg != "" {
		t.Errorf("thinkingFor(unknown) = %+v, want disabled with no kwarg", got)
	}
}

// TestGenerationThinkingIsNilWhenUnwired: an unconfigured service must send no
// directive at all rather than a zero-value one.
func TestGenerationThinkingIsNilWhenUnwired(t *testing.T) {
	s := &Service{}
	if got := s.thinkingFor("any"); got != nil {
		t.Errorf("unwired thinkingFor = %+v, want nil", got)
	}
}
