package handlers

import (
	"strings"
	"testing"
)

func TestHandleSubagentsListAction_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		runs        []SubagentRunRecord
		wantContain []string
	}{
		{
			name: "shows active and recent entries",
			runs: []SubagentRunRecord{
				{RunID: "run1", ChildSessionKey: "k1", Label: "worker", Task: "do stuff", CreatedAt: 100, StartedAt: 100},
				{RunID: "run2", ChildSessionKey: "k2", Label: "done-one", Task: "old task", CreatedAt: currentTimeMs() - 60000, StartedAt: currentTimeMs() - 60000, EndedAt: currentTimeMs() - 30000},
			},
			wantContain: []string{"active subagents:", "worker", "recent subagents"},
		},
		{
			name:        "shows empty states",
			runs:        nil,
			wantContain: []string{"active subagents:", "(none)", "recent subagents"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HandleSubagentsListAction(&SubagentsCommandContext{Runs: tt.runs})
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(result.Reply, want) {
					t.Fatalf("expected reply to contain %q, got: %s", want, result.Reply)
				}
			}
		})
	}
}
