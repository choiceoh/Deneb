package handlers

import (
	"strings"
	"testing"
)

func TestResolveHandledPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/subagents list", SubagentsCmdPrefix},
		{"/subagents", SubagentsCmdPrefix},
		{"/kill 1", SubagentsCmdKill},
		{"/steer 1 hello", SubagentsCmdSteer},
		{"/tell 1 hello", SubagentsCmdTell},
		{"/focus my-agent", SubagentsCmdFocus},
		{"/unfocus", SubagentsCmdUnfocus},
		{"/agents", SubagentsCmdAgents},
		{"/unknown", ""},
		{"hello", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := ResolveHandledPrefix(tt.input)
		if got != tt.want {
			t.Errorf("ResolveHandledPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveSubagentsAction(t *testing.T) {
	tests := []struct {
		prefix string
		tokens []string
		want   SubagentsAction
	}{
		{SubagentsCmdPrefix, []string{"list"}, SubagentsActionList},
		{SubagentsCmdPrefix, []string{"kill", "1"}, SubagentsActionKill},
		{SubagentsCmdPrefix, []string{"spawn", "agent1", "do", "stuff"}, SubagentsActionSpawn},
		{SubagentsCmdPrefix, []string{}, SubagentsActionList},
		{SubagentsCmdPrefix, []string{"unknown"}, ""},
		{SubagentsCmdKill, []string{"1"}, SubagentsActionKill},
		{SubagentsCmdSteer, []string{"1", "go"}, SubagentsActionSteer},
		{SubagentsCmdFocus, []string{"agent"}, SubagentsActionFocus},
		{SubagentsCmdUnfocus, nil, SubagentsActionUnfocus},
		{SubagentsCmdAgents, nil, SubagentsActionAgents},
	}

	for _, tt := range tests {
		tokens := make([]string, len(tt.tokens))
		copy(tokens, tt.tokens)
		got, _ := ResolveSubagentsAction(tt.prefix, tokens)
		if got != tt.want {
			t.Errorf("ResolveSubagentsAction(%q, %v) = %q, want %q", tt.prefix, tt.tokens, got, tt.want)
		}
	}
}

func TestResolveSubagentTarget(t *testing.T) {
	runs := []SubagentRunRecord{
		{RunID: "abc12345-long-id", ChildSessionKey: "key:sub:1", Label: "alpha", CreatedAt: 100},
		{RunID: "def67890-long-id", ChildSessionKey: "key:sub:2", Label: "beta", CreatedAt: 200, EndedAt: 300},
		{RunID: "ghi11111-long-id", ChildSessionKey: "key:sub:3", Label: "gamma", CreatedAt: 300},
	}

	// By index.
	entry, err := ResolveSubagentTarget(runs, "1")
	if err != "" || entry == nil {
		t.Fatalf("expected entry for index 1, got err=%q", err)
	}

	// By label.
	entry, err = ResolveSubagentTarget(runs, "alpha")
	if err != "" || entry == nil || entry.Label != "alpha" {
		t.Fatalf("expected alpha, got err=%q entry=%v", err, entry)
	}

	// By runId prefix.
	entry, err = ResolveSubagentTarget(runs, "abc12345")
	if err != "" || entry == nil || entry.RunID != "abc12345-long-id" {
		t.Fatalf("expected abc12345, got err=%q", err)
	}

	// By session key.
	entry, err = ResolveSubagentTarget(runs, "key:sub:2")
	if err != "" || entry == nil || entry.ChildSessionKey != "key:sub:2" {
		t.Fatalf("expected key:sub:2, got err=%q", err)
	}

	// Unknown.
	entry, err = ResolveSubagentTarget(runs, "nonexistent")
	if entry != nil {
		t.Fatalf("expected nil entry for unknown target")
	}
	if err == "" {
		t.Fatalf("expected error for unknown target")
	}

	// Missing.
	entry, err = ResolveSubagentTarget(runs, "")
	if entry != nil || err == "" {
		t.Fatalf("expected error for empty target")
	}
}

func TestHandleSubagentsListAction(t *testing.T) {
	runs := []SubagentRunRecord{
		{RunID: "run1", ChildSessionKey: "k1", Label: "worker", Task: "do stuff", CreatedAt: 100, StartedAt: 100},
		{RunID: "run2", ChildSessionKey: "k2", Label: "done-one", Task: "old task", CreatedAt: 50, StartedAt: 50, EndedAt: 80},
	}
	ctx := &SubagentsCommandContext{
		Runs: runs,
	}
	result := HandleSubagentsListAction(ctx)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.Reply, "worker") {
		t.Errorf("expected 'worker' in reply, got: %s", result.Reply)
	}
	if !strings.Contains(result.Reply, "active subagents:") {
		t.Errorf("expected 'active subagents:' in reply")
	}
}

func TestHandleSubagentsInfoAction(t *testing.T) {
	now := currentTimeMs()
	runs := []SubagentRunRecord{
		{RunID: "run123abc", ChildSessionKey: "k1", Label: "worker", Task: "important task", CreatedAt: now - 60000, StartedAt: now - 60000, Cleanup: "keep"},
	}
	ctx := &SubagentsCommandContext{
		Runs:       runs,
		RestTokens: []string{"1"},
	}
	result := HandleSubagentsInfoAction(ctx)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.Reply, "worker") {
		t.Errorf("expected 'worker' in reply")
	}
	if !strings.Contains(result.Reply, "important task") {
		t.Errorf("expected 'important task' in reply")
	}
	if !strings.Contains(result.Reply, "Cleanup: keep") {
		t.Errorf("expected 'Cleanup: keep' in reply")
	}
}

func TestHandleSubagentsCommand_nil_for_non_commands(t *testing.T) {
	result := HandleSubagentsCommand("hello world", "key", "telegram", "acc", "", "sender", false, true, nil)
	if result != nil {
		t.Errorf("expected nil for non-command, got: %+v", result)
	}
}

func TestHandleSubagentsCommand_help_for_unknown_action(t *testing.T) {
	result := HandleSubagentsCommand("/subagents badaction", "key", "telegram", "acc", "", "sender", false, true, &SubagentCommandDeps{
		ListRuns: func(string) []SubagentRunRecord { return nil },
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.Reply, "Usage:") {
		t.Errorf("expected help text, got: %s", result.Reply)
	}
}
