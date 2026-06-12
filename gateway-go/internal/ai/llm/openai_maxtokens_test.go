package llm

import "testing"

// TestApplySamplingParams_DisabledTemplateKwarg: a template-toggle "disabled"
// emits chat_template_kwargs and suppresses reasoning_effort entirely; without
// the kwarg the per-model reasoning_effort floor ("low") is preserved.
func TestApplySamplingParams_DisabledTemplateKwarg(t *testing.T) {
	oaiReq := &openAIRequest{MaxTokens: 1024}
	applySamplingParams(oaiReq, &ChatRequest{Thinking: &ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"}})
	if oaiReq.ReasoningEffort != "" {
		t.Errorf("ReasoningEffort = %q, want empty with a template toggle", oaiReq.ReasoningEffort)
	}
	v, ok := oaiReq.ChatTemplateKwargs["thinking"]
	if !ok || v != false {
		t.Fatalf("ChatTemplateKwargs = %#v, want {thinking:false}", oaiReq.ChatTemplateKwargs)
	}

	floor := &openAIRequest{MaxTokens: 1024}
	applySamplingParams(floor, &ChatRequest{Thinking: &ThinkingConfig{Type: "disabled"}})
	if floor.ReasoningEffort != "low" {
		t.Errorf("no-kwarg disabled must keep the low floor, got %q", floor.ReasoningEffort)
	}
	if floor.ChatTemplateKwargs != nil {
		t.Errorf("no-kwarg disabled must not emit chat_template_kwargs: %#v", floor.ChatTemplateKwargs)
	}
}
