package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestNormalizeThinkLevel(t *testing.T) {
	tests := []struct {
		raw    string
		want   types.ThinkLevel
		wantOk bool
	}{
		{"off", types.ThinkOff, true},
		{"on", types.ThinkLow, true},
		{"enable", types.ThinkLow, true},
		{"min", types.ThinkMinimal, true},
		{"minimal", types.ThinkMinimal, true},
		{"low", types.ThinkLow, true},
		{"medium", types.ThinkMedium, true},
		{"med", types.ThinkMedium, true},
		{"high", types.ThinkHigh, true},
		{"max", types.ThinkHigh, true},
		{"xhigh", types.ThinkXHigh, true},
		{"extrahigh", types.ThinkXHigh, true},
		{"adaptive", types.ThinkAdaptive, true},
		{"auto", types.ThinkAdaptive, true},
		{"think", types.ThinkMinimal, true},
		{"", "", false},
		{"invalid", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := types.NormalizeThinkLevel(tt.raw)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("types.NormalizeThinkLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestNormalizeVerboseLevel(t *testing.T) {
	tests := []struct {
		raw  string
		want types.VerboseLevel
		ok   bool
	}{
		{"off", types.VerboseOff, true},
		{"on", types.VerboseOn, true},
		{"full", types.VerboseFull, true},
		{"false", types.VerboseOff, true},
		{"everything", types.VerboseFull, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := types.NormalizeVerboseLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("types.NormalizeVerboseLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeElevatedLevel(t *testing.T) {
	tests := []struct {
		raw  string
		want types.ElevatedLevel
		ok   bool
	}{
		{"off", types.ElevatedOff, true},
		{"on", types.ElevatedOn, true},
		{"ask", types.ElevatedAsk, true},
		{"full", types.ElevatedFull, true},
		{"auto-approve", types.ElevatedFull, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := types.NormalizeElevatedLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("types.NormalizeElevatedLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeReasoningLevel(t *testing.T) {
	tests := []struct {
		raw  string
		want types.ReasoningLevel
		ok   bool
	}{
		{"off", types.ReasoningOff, true},
		{"on", types.ReasoningOn, true},
		{"stream", types.ReasoningStream, true},
		{"hidden", types.ReasoningOff, true},
		{"visible", types.ReasoningOn, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := types.NormalizeReasoningLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("types.NormalizeReasoningLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeFastMode(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
		ok   bool
	}{
		{"on", true, true},
		{"off", false, true},
		{"fast", true, true},
		{"normal", false, true},
		{"", false, false},
	}
	for _, tt := range tests {
		got, ok := types.NormalizeFastMode(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("types.NormalizeFastMode(%q) = (%v, %v), want (%v, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeProviderID(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"anthropic", "anthropic"},
		{"z.ai", "zai"},
		{"z-ai", "zai"},
		{"bedrock", "amazon-bedrock"},
		{"aws-bedrock", "amazon-bedrock"},
		{"OpenAI", "openai"},
		{"", ""},
	}
	for _, tt := range tests {
		got := types.NormalizeProviderID(tt.raw)
		if got != tt.want {
			t.Errorf("types.NormalizeProviderID(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestIsBinaryThinkingProvider(t *testing.T) {
	if !types.IsBinaryThinkingProvider("z.ai") {
		t.Error("z.ai should be binary")
	}
	if types.IsBinaryThinkingProvider("anthropic") {
		t.Error("anthropic should not be binary")
	}
}
