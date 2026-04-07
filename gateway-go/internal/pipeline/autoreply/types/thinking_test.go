package types

import (
	"testing"
)

func TestNormalizeThinkLevel(t *testing.T) {
	tests := []struct {
		input  string
		want   ThinkLevel
		wantOk bool
	}{
		// Direct matches.
		{"off", ThinkOff, true},
		{"minimal", ThinkMinimal, true},
		{"low", ThinkLow, true},
		{"medium", ThinkMedium, true},
		{"high", ThinkHigh, true},
		{"adaptive", ThinkAdaptive, true},

		// Aliases.
		{"on", ThinkLow, true},
		{"enable", ThinkLow, true},
		{"enabled", ThinkLow, true},
		{"min", ThinkMinimal, true},
		{"think", ThinkMinimal, true},
		{"mid", ThinkMedium, true},
		{"med", ThinkMedium, true},
		{"harder", ThinkMedium, true},
		{"ultra", ThinkHigh, true},
		{"max", ThinkHigh, true},
		{"highest", ThinkHigh, true},
		{"auto", ThinkAdaptive, true},
		{"xhigh", ThinkXHigh, true},
		{"extrahigh", ThinkXHigh, true},

		// Compound aliases.
		{"thinkhard", ThinkLow, true},
		{"think-hard", ThinkLow, true},
		{"think_hard", ThinkLow, true},
		{"thinkharder", ThinkMedium, true},
		{"think-harder", ThinkMedium, true},
		{"thinkhardest", ThinkHigh, true},
		{"ultrathink", ThinkHigh, true},

		// Invalid.
		{"", ThinkLevel(""), false},
		{"invalid", ThinkLevel(""), false},
		{"super", ThinkLevel(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeThinkLevel(tc.input)
			if ok != tc.wantOk {
				t.Errorf("NormalizeThinkLevel(%q) ok = %v, want %v", tc.input, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("NormalizeThinkLevel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeVerboseLevel(t *testing.T) {
	tests := []struct {
		input  string
		want   VerboseLevel
		wantOk bool
	}{
		{"off", VerboseOff, true},
		{"false", VerboseOff, true},
		{"no", VerboseOff, true},
		{"0", VerboseOff, true},
		{"on", VerboseOn, true},
		{"minimal", VerboseOn, true},
		{"true", VerboseOn, true},
		{"yes", VerboseOn, true},
		{"1", VerboseOn, true},
		{"full", VerboseFull, true},
		{"all", VerboseFull, true},
		{"everything", VerboseFull, true},
		{"", VerboseLevel(""), false},
		{"unknown", VerboseLevel(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeVerboseLevel(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeElevatedLevel(t *testing.T) {
	tests := []struct {
		input  string
		want   ElevatedLevel
		wantOk bool
	}{
		{"off", ElevatedOff, true},
		{"false", ElevatedOff, true},
		{"no", ElevatedOff, true},
		{"0", ElevatedOff, true},
		{"on", ElevatedOn, true},
		{"true", ElevatedOn, true},
		{"yes", ElevatedOn, true},
		{"1", ElevatedOn, true},
		{"ask", ElevatedAsk, true},
		{"prompt", ElevatedAsk, true},
		{"approval", ElevatedAsk, true},
		{"approve", ElevatedAsk, true},
		{"full", ElevatedFull, true},
		{"auto", ElevatedFull, true},
		{"auto-approve", ElevatedFull, true},
		{"autoapprove", ElevatedFull, true},
		{"", ElevatedLevel(""), false},
		{"invalid", ElevatedLevel(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeElevatedLevel(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeReasoningLevel(t *testing.T) {
	tests := []struct {
		input  string
		want   ReasoningLevel
		wantOk bool
	}{
		{"off", ReasoningOff, true},
		{"false", ReasoningOff, true},
		{"hide", ReasoningOff, true},
		{"disabled", ReasoningOff, true},
		{"on", ReasoningOn, true},
		{"true", ReasoningOn, true},
		{"show", ReasoningOn, true},
		{"enabled", ReasoningOn, true},
		{"stream", ReasoningStream, true},
		{"streaming", ReasoningStream, true},
		{"draft", ReasoningStream, true},
		{"live", ReasoningStream, true},
		{"", ReasoningLevel(""), false},
		{"unknown", ReasoningLevel(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeReasoningLevel(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeFastMode(t *testing.T) {
	tests := []struct {
		input  string
		want   bool
		wantOk bool
	}{
		{"off", false, true},
		{"false", false, true},
		{"no", false, true},
		{"0", false, true},
		{"disable", false, true},
		{"normal", false, true},
		{"on", true, true},
		{"true", true, true},
		{"yes", true, true},
		{"1", true, true},
		{"enable", true, true},
		{"fast", true, true},
		{"", false, false},
		{"unknown", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeFastMode(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeUsageDisplay(t *testing.T) {
	tests := []struct {
		input  string
		want   UsageDisplayLevel
		wantOk bool
	}{
		{"off", UsageOff, true},
		{"false", UsageOff, true},
		{"disable", UsageOff, true},
		{"on", UsageTokens, true},
		{"true", UsageTokens, true},
		{"tokens", UsageTokens, true},
		{"token", UsageTokens, true},
		{"tok", UsageTokens, true},
		{"minimal", UsageTokens, true},
		{"full", UsageFull, true},
		{"session", UsageFull, true},
		{"", UsageDisplayLevel(""), false},
		{"invalid", UsageDisplayLevel(""), false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeUsageDisplay(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeProviderID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"OpenAI", "openai"},
		{"z.ai", "zai"},
		{"z-ai", "zai"},
		{"bedrock", "amazon-bedrock"},
		{"aws-bedrock", "amazon-bedrock"},
		{"  Anthropic  ", "anthropic"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeProviderID(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeProviderID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsBinaryThinkingProvider(t *testing.T) {
	if !IsBinaryThinkingProvider("z.ai") {
		t.Error("expected z.ai to be binary thinking provider")
	}
	if !IsBinaryThinkingProvider("z-ai") {
		t.Error("expected z-ai to be binary thinking provider")
	}
	if IsBinaryThinkingProvider("openai") {
		t.Error("expected openai not to be binary thinking provider")
	}
}

func TestListThinkingLevelLabels(t *testing.T) {
	// Binary provider.
	labels := ListThinkingLevelLabels("z.ai")
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels for binary provider, got %d", len(labels))
	}
	if labels[0] != "off" || labels[1] != "on" {
		t.Errorf("expected [off, on], got %v", labels)
	}

	// Normal provider.
	labels = ListThinkingLevelLabels("openai")
	if len(labels) != 6 {
		t.Fatalf("expected 6 labels for normal provider, got %d", len(labels))
	}
}

func TestFormatThinkingLevels(t *testing.T) {
	got := FormatThinkingLevels("z.ai", ", ")
	if got != "off, on" {
		t.Errorf("expected 'off, on', got %q", got)
	}

	// Default separator.
	got = FormatThinkingLevels("z.ai", "")
	if got != "off, on" {
		t.Errorf("expected 'off, on' with default separator, got %q", got)
	}

	// Custom separator.
	got = FormatThinkingLevels("z.ai", " | ")
	if got != "off | on" {
		t.Errorf("expected 'off | on', got %q", got)
	}
}

func TestBaseThinkingLevels(t *testing.T) {
	levels := BaseThinkingLevels()
	if len(levels) != 6 {
		t.Fatalf("expected 6 base thinking levels, got %d", len(levels))
	}
	// Should not include xhigh.
	for _, l := range levels {
		if l == ThinkXHigh {
			t.Error("base thinking levels should not include xhigh")
		}
	}
}
