package handlers

import (
	"errors"
	"strings"
	"testing"
)

func TestHandleSubagentsKillAction_TableDriven(t *testing.T) {
	baseRuns := []SubagentRunRecord{
		{RunID: "run-active-123", ChildSessionKey: "k1", Label: "active", CreatedAt: 100, StartedAt: 100},
		{RunID: "run-done-456", ChildSessionKey: "k2", Label: "done", CreatedAt: 200, StartedAt: 200, EndedAt: 300},
	}

	tests := []struct {
		name       string
		ctx        *SubagentsCommandContext
		deps       *SubagentKillDeps
		wantStop   bool
		wantInText string
	}{
		{
			name:       "usage for /subagents prefix",
			ctx:        &SubagentsCommandContext{HandledPrefix: SubagentsCmdPrefix, Runs: baseRuns},
			wantStop:   true,
			wantInText: "Usage: /subagents kill",
		},
		{
			name:       "kill all success",
			ctx:        &SubagentsCommandContext{HandledPrefix: SubagentsCmdPrefix, RestTokens: []string{"all"}, Runs: baseRuns},
			deps:       &SubagentKillDeps{KillAll: func(controllerKey string, runs []SubagentRunRecord) (int, error) { return 1, nil }},
			wantStop:   true,
			wantInText: "Killed 1 subagent(s).",
		},
		{
			name:       "specific run already finished",
			ctx:        &SubagentsCommandContext{HandledPrefix: SubagentsCmdPrefix, RestTokens: []string{"done"}, Runs: baseRuns},
			deps:       &SubagentKillDeps{KillRun: func(runID string) (bool, error) { return true, nil }},
			wantStop:   true,
			wantInText: "already finished",
		},
		{
			name:       "kill run dependency error",
			ctx:        &SubagentsCommandContext{HandledPrefix: SubagentsCmdPrefix, RestTokens: []string{"active"}, Runs: baseRuns},
			deps:       &SubagentKillDeps{KillRun: func(runID string) (bool, error) { return false, errors.New("boom") }},
			wantStop:   true,
			wantInText: "⚠️ boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleSubagentsKillAction(tt.ctx, tt.deps)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.ShouldStop != tt.wantStop {
				t.Fatalf("ShouldStop=%v want %v", got.ShouldStop, tt.wantStop)
			}
			if tt.wantInText != "" && !strings.Contains(got.Reply, tt.wantInText) {
				t.Fatalf("expected reply to contain %q, got: %s", tt.wantInText, got.Reply)
			}
		})
	}
}
