package handlers

import (
	"strings"
	"testing"
)

func TestHandleSubagentsInfoAction_TableDriven(t *testing.T) {
	now := currentTimeMs()
	runs := []SubagentRunRecord{
		{RunID: "run123abc", ChildSessionKey: "k1", Label: "worker", Task: "important task", CreatedAt: now - 60000, StartedAt: now - 60000, Cleanup: "keep"},
	}

	tests := []struct {
		name       string
		restTokens []string
		wantInText []string
	}{
		{
			name:       "usage when target missing",
			restTokens: nil,
			wantInText: []string{"Usage: /subagents info"},
		},
		{
			name:       "unknown target",
			restTokens: []string{"missing"},
			wantInText: []string{"⚠️"},
		},
		{
			name:       "shows details",
			restTokens: []string{"1"},
			wantInText: []string{"worker", "important task", "Cleanup: keep", "Outcome:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HandleSubagentsInfoAction(&SubagentsCommandContext{Runs: runs, RestTokens: tt.restTokens})
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			for _, want := range tt.wantInText {
				if !strings.Contains(result.Reply, want) {
					t.Fatalf("expected reply to contain %q, got: %s", want, result.Reply)
				}
			}
		})
	}
}
