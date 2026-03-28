package thinking

import (
	"testing"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestSupportsBuiltInXHighThinking(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{"openai", "gpt-5.4", true},
		{"openai", "gpt-5.4-pro", true},
		{"openai", "gpt-5.4-mini", true},
		{"openai", "gpt-5.2", true},
		{"openai", "gpt-4o", false},
		{"openai", "", false},
		{"openai-codex", "gpt-5.4", true},
		{"openai-codex", "gpt-5.3-codex", true},
		{"openai-codex", "gpt-5.3-codex-spark", true},
		{"openai-codex", "gpt-4o", false},
		{"anthropic", "claude-opus-4-6", false},
		{"", "gpt-5.4", false},
	}

	for _, tt := range tests {
		name := tt.provider + "/" + tt.model
		t.Run(name, func(t *testing.T) {
			got := SupportsBuiltInXHighThinking(tt.provider, tt.model)
			if got != tt.want {
				t.Errorf("SupportsBuiltInXHighThinking(%q, %q) = %v, want %v",
					tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveThinkingDefaultForModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		catalog  []ThinkingCatalogEntry
		want     types.ThinkLevel
	}{
		{
			name:     "anthropic claude 4.6 defaults to adaptive",
			provider: "anthropic",
			model:    "claude-opus-4.6",
			want:     types.ThinkAdaptive,
		},
		{
			name:     "anthropic claude 4-6 variant defaults to adaptive",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			want:     types.ThinkAdaptive,
		},
		{
			name:     "amazon-bedrock claude 4.6 defaults to adaptive",
			provider: "amazon-bedrock",
			model:    "claude-opus-4.6",
			want:     types.ThinkAdaptive,
		},
		{
			name:     "catalog model with reasoning defaults to low",
			provider: "custom",
			model:    "my-model",
			catalog:  []ThinkingCatalogEntry{{Provider: "custom", ID: "my-model", Reasoning: true}},
			want:     types.ThinkLow,
		},
		{
			name:     "unknown model defaults to off",
			provider: "openai",
			model:    "gpt-4o",
			want:     types.ThinkOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveThinkingDefaultForModel(tt.provider, tt.model, tt.catalog)
			if got != tt.want {
				t.Errorf("ResolveThinkingDefaultForModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListThinkingLevels_WithXHigh(t *testing.T) {
	levels := ListThinkingLevels("openai", "gpt-5.4")
	found := false
	for _, l := range levels {
		if l == types.ThinkXHigh {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected xhigh in thinking levels for supported model")
	}
	// xhigh should come before adaptive.
	for i, l := range levels {
		if l == types.ThinkXHigh {
			if i+1 >= len(levels) || levels[i+1] != types.ThinkAdaptive {
				t.Error("xhigh should be inserted before adaptive")
			}
			break
		}
	}
}

func TestListThinkingLevels_WithoutXHigh(t *testing.T) {
	levels := ListThinkingLevels("anthropic", "claude-opus-4.6")
	for _, l := range levels {
		if l == types.ThinkXHigh {
			t.Error("unexpected xhigh for anthropic model")
		}
	}
}

func TestResolveResponseUsageMode(t *testing.T) {
	tests := []struct {
		raw  string
		want types.UsageDisplayLevel
	}{
		{"", types.UsageOff},
		{"tokens", types.UsageTokens},
		{"full", types.UsageFull},
		{"off", types.UsageOff},
		{"garbage", types.UsageOff},
	}

	for _, tt := range tests {
		got := ResolveResponseUsageMode(tt.raw)
		if got != tt.want {
			t.Errorf("ResolveResponseUsageMode(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestFormatXHighModelHint(t *testing.T) {
	hint := FormatXHighModelHint()
	if hint == "" {
		t.Error("expected non-empty hint")
	}
}
