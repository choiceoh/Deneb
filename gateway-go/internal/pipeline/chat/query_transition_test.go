package chat

import (
	"fmt"
	"testing"
)

func TestQueryTransition(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		tr := NewTerminal(TerminalCompleted, nil)
		if !tr.IsTerminal() {
			t.Error("expected terminal")
		}
		if tr.Reason() != "completed" {
			t.Errorf("reason = %q, want completed", tr.Reason())
		}
		if tr.Error != nil {
			t.Error("expected nil error")
		}
	})

	t.Run("terminal with error", func(t *testing.T) {
		tr := NewTerminal(TerminalModelError, fmt.Errorf("timeout"))
		if !tr.IsTerminal() {
			t.Error("expected terminal")
		}
		if tr.Error == nil {
			t.Error("expected error")
		}
	})

	t.Run("continue", func(t *testing.T) {
		tr := NewContinue(ContinueToolUse)
		if tr.IsTerminal() {
			t.Error("expected continue")
		}
		if tr.Reason() != "tool_use" {
			t.Errorf("reason = %q, want tool_use", tr.Reason())
		}
	})

	t.Run("zero value", func(t *testing.T) {
		var tr QueryTransition
		if tr.IsTerminal() {
			t.Error("zero value should not be terminal")
		}
		if tr.Reason() != "unknown" {
			t.Errorf("reason = %q, want unknown", tr.Reason())
		}
	})
}
