package types

import (
	"testing"
)

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
