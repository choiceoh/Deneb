package autoreply

import "testing"

func TestNormalizeThinkLevel(t *testing.T) {
	tests := []struct {
		raw    string
		want   ThinkLevel
		wantOk bool
	}{
		{"off", ThinkOff, true},
		{"on", ThinkLow, true},
		{"enable", ThinkLow, true},
		{"min", ThinkMinimal, true},
		{"minimal", ThinkMinimal, true},
		{"low", ThinkLow, true},
		{"medium", ThinkMedium, true},
		{"med", ThinkMedium, true},
		{"high", ThinkHigh, true},
		{"max", ThinkHigh, true},
		{"xhigh", ThinkXHigh, true},
		{"extrahigh", ThinkXHigh, true},
		{"adaptive", ThinkAdaptive, true},
		{"auto", ThinkAdaptive, true},
		{"think", ThinkMinimal, true},
		{"", "", false},
		{"invalid", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := NormalizeThinkLevel(tt.raw)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("NormalizeThinkLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestNormalizeVerboseLevel(t *testing.T) {
	tests := []struct{ raw string; want VerboseLevel; ok bool }{
		{"off", VerboseOff, true},
		{"on", VerboseOn, true},
		{"full", VerboseFull, true},
		{"false", VerboseOff, true},
		{"everything", VerboseFull, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeVerboseLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeVerboseLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeElevatedLevel(t *testing.T) {
	tests := []struct{ raw string; want ElevatedLevel; ok bool }{
		{"off", ElevatedOff, true},
		{"on", ElevatedOn, true},
		{"ask", ElevatedAsk, true},
		{"full", ElevatedFull, true},
		{"auto-approve", ElevatedFull, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeElevatedLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeElevatedLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeReasoningLevel(t *testing.T) {
	tests := []struct{ raw string; want ReasoningLevel; ok bool }{
		{"off", ReasoningOff, true},
		{"on", ReasoningOn, true},
		{"stream", ReasoningStream, true},
		{"hidden", ReasoningOff, true},
		{"visible", ReasoningOn, true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeReasoningLevel(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeReasoningLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeFastMode(t *testing.T) {
	tests := []struct{ raw string; want bool; ok bool }{
		{"on", true, true},
		{"off", false, true},
		{"fast", true, true},
		{"normal", false, true},
		{"", false, false},
	}
	for _, tt := range tests {
		got, ok := NormalizeFastMode(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeFastMode(%q) = (%v, %v), want (%v, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeProviderId(t *testing.T) {
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
		got := NormalizeProviderId(tt.raw)
		if got != tt.want {
			t.Errorf("NormalizeProviderId(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestIsBinaryThinkingProvider(t *testing.T) {
	if !IsBinaryThinkingProvider("z.ai") {
		t.Error("z.ai should be binary")
	}
	if IsBinaryThinkingProvider("anthropic") {
		t.Error("anthropic should not be binary")
	}
}
