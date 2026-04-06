package rlm

import (
	"testing"
)

func TestSubAgentSystemPrompt_Empty(t *testing.T) {
	text := SubAgentSystemPrompt()
	if text != "" {
		t.Errorf("SubAgentSystemPrompt should be empty, got %d chars", len(text))
	}
}
