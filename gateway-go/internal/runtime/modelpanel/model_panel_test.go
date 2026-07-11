package modelpanel

import (
	"context"
	"testing"
)

func TestConsultWithoutRegistryDegradesToEmpty(t *testing.T) {
	panel := New(nil, nil)
	if got := panel.Consult(context.Background(), "system", "question", []string{"qwen3"}); len(got) != 0 {
		t.Fatalf("Consult without registry = %v, want empty", got)
	}
}

func TestPanelModelFamily(t *testing.T) {
	tests := map[string]string{
		"deepseek-v4":         "deepseek",
		"Qwen3.6-35B":         "qwen",
		"glm-4.5-air":         "glm",
		"MiMo-V2":             "mimo",
		"kimi-k2":             "kimi",
		"gemma-3-27b":         "gemma",
		"gpt-5.2":             "openai",
		"claude-sonnet-4.5":   "anthropic",
		"gemini-2.5-pro":      "gemini",
		"custom-router-model": "custom-router-model",
	}
	for model, want := range tests {
		if got := panelModelFamily(model); got != want {
			t.Errorf("panelModelFamily(%q) = %q, want %q", model, got, want)
		}
	}
}
