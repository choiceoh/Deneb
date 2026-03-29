package handlers

import (
	"strings"
	"testing"
)

func TestHandleSubagentsHelpAction(t *testing.T) {
	tests := []struct {
		name     string
		contains string
	}{
		{name: "contains usage header", contains: "Usage:"},
		{name: "contains list command", contains: "/subagents list"},
	}

	result := HandleSubagentsHelpAction()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(result.Reply, tt.contains) {
				t.Fatalf("expected help to contain %q, got: %s", tt.contains, result.Reply)
			}
		})
	}
}
