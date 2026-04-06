package rlm

import (
	"strings"
	"testing"
)

func TestSubAgentSystemPrompt_Compact(t *testing.T) {
	text := SubAgentSystemPrompt()
	if !strings.Contains(text, "한국어로 답변") {
		t.Error("expected Korean instruction in SubAgentSystemPrompt")
	}
	// Sub-agent prompt should be compact (under 500 chars).
	if len(text) > 500 {
		t.Errorf("SubAgentSystemPrompt too long: %d chars (want <500)", len(text))
	}
}
