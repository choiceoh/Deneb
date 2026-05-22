package modelrole

import (
	"log/slog"
	"testing"
)

func TestIsReasoningModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"qwen3.6-35b-a3b", true},
		{"Qwen3.6-35B-A3B", true},
		{"vllm/qwen3.6-35b-a3b", true},
		{"qwen3-30b-a3b", true},
		{"qwen3-30b-a3b-thinking-2507", true},
		{"qwen3-30b-a3b-instruct-2507", false}, // explicit non-thinking variant
		{"qwq-32b", true},
		{"deepseek-r1", true},
		{"deepseek-r1-distill-qwen-7b", true},
		{"deepseek-reasoner", true},
		{"gpt-oss-20b", true},
		{"gemma4", false},
		{"glm-5-turbo", false},
		{"qwen2.5-72b-instruct", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := IsReasoningModel(tt.model); got != tt.want {
				t.Errorf("IsReasoningModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestRoleIsReasoning(t *testing.T) {
	// Lightweight and Fallback both default to the local vLLM model.
	reg := NewRegistry(slog.Default(), "zai/test-model", "qwen3.6-35b-a3b")
	if !reg.RoleIsReasoning(RoleLightweight) {
		t.Error("RoleLightweight with qwen3.6 model: want reasoning")
	}
	if !reg.RoleIsReasoning(RoleFallback) {
		t.Error("RoleFallback with qwen3.6 model: want reasoning")
	}
	// Main is a non-reasoning zai model.
	if reg.RoleIsReasoning(RoleMain) {
		t.Error("RoleMain with zai/test-model: want non-reasoning")
	}

	regGemma := NewRegistry(slog.Default(), "zai/test-model", "gemma4")
	if regGemma.RoleIsReasoning(RoleLightweight) {
		t.Error("RoleLightweight with gemma4 model: want non-reasoning")
	}
}
