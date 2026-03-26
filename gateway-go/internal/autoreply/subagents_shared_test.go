package autoreply

import (
	"strings"
	"testing"
)

func TestResolveDisplayStatus(t *testing.T) {
	// Running with no pending.
	r := &SubagentRunRecord{}
	if got := ResolveDisplayStatus(r, 0); got != "running" {
		t.Errorf("running = %q", got)
	}

	// Running with pending descendants.
	if got := ResolveDisplayStatus(r, 2); got != "active (waiting on 2 children)" {
		t.Errorf("pending 2 = %q", got)
	}

	// Single child.
	if got := ResolveDisplayStatus(r, 1); got != "active (waiting on 1 child)" {
		t.Errorf("pending 1 = %q", got)
	}

	// Error status.
	ended := int64(100)
	r = &SubagentRunRecord{EndedAt: &ended, Outcome: &SubagentRunOutcome{Status: "error"}}
	if got := ResolveDisplayStatus(r, 0); got != "failed" {
		t.Errorf("error = %q", got)
	}
}

func TestResolveHandledPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/subagents list", "/subagents"},
		{"/kill 1", "/kill"},
		{"/focus worker", "/focus"},
		{"/unfocus", "/unfocus"},
		{"/agents", "/agents"},
		{"/steer 1 do it", "/steer"},
		{"/tell 1 hi", "/tell"},
		{"/model gpt", ""},
		{"hello", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ResolveHandledPrefix(tt.input); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSubagentsAction(t *testing.T) {
	// /subagents with no args defaults to list.
	action, _ := ResolveSubagentsAction("/subagents", nil)
	if action != ActionList {
		t.Errorf("default = %q", action)
	}

	// /subagents kill.
	action, rest := ResolveSubagentsAction("/subagents", []string{"kill", "1"})
	if action != ActionKill || len(rest) != 1 || rest[0] != "1" {
		t.Errorf("kill: action=%q rest=%v", action, rest)
	}

	// /kill prefix.
	action, _ = ResolveSubagentsAction("/kill", []string{"all"})
	if action != ActionKill {
		t.Errorf("/kill = %q", action)
	}

	// /focus prefix.
	action, _ = ResolveSubagentsAction("/focus", []string{"worker"})
	if action != ActionFocus {
		t.Errorf("/focus = %q", action)
	}

	// Invalid action.
	action, _ = ResolveSubagentsAction("/subagents", []string{"invalid"})
	if action != "" {
		t.Errorf("invalid = %q", action)
	}
}

func TestBuildSubagentsHelp(t *testing.T) {
	help := BuildSubagentsHelp()
	if !strings.Contains(help, "/subagents list") {
		t.Error("help should contain /subagents list")
	}
	if !strings.Contains(help, "/kill") {
		t.Error("help should contain /kill")
	}
}

func TestBuildSubagentRunListEntries(t *testing.T) {
	now := timeNowMs()
	started := now - 5000
	runs := []*SubagentRunRecord{
		{RunID: "r1", Label: "worker", CreatedAt: now - 10000, StartedAt: &started},
		{RunID: "r2", Label: "builder", CreatedAt: now - 20000, StartedAt: &started, EndedAt: &now},
	}

	active, recent := BuildSubagentRunListEntries(runs, 60, 72)
	if len(active) != 1 {
		t.Errorf("active count = %d", len(active))
	}
	if len(recent) != 1 {
		t.Errorf("recent count = %d", len(recent))
	}
	if active[0].Label != "worker" {
		t.Errorf("active label = %q", active[0].Label)
	}
	if !strings.Contains(active[0].Line, "worker") {
		t.Errorf("active line = %q", active[0].Line)
	}
}

func TestFormatSubagentInfo(t *testing.T) {
	started := int64(1000000)
	run := &SubagentRunRecord{
		RunID:           "run-abc",
		ChildSessionKey: "sess:1",
		Label:           "my worker",
		Task:            "do stuff",
		Cleanup:         "delete",
		CreatedAt:       900000,
		StartedAt:       &started,
		Outcome:         &SubagentRunOutcome{Status: "ok"},
	}

	info := FormatSubagentInfo(run, 0)
	if !strings.Contains(info, "my worker") {
		t.Error("should contain label")
	}
	if !strings.Contains(info, "run-abc") {
		t.Error("should contain run ID")
	}
	if !strings.Contains(info, "sess:1") {
		t.Error("should contain session key")
	}
	if !strings.Contains(info, "delete") {
		t.Error("should contain cleanup")
	}
}

func TestFormatDurationCompact(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "n/a"},
		{-1, "n/a"},
		{500, "500ms"},
		{5000, "5.0s"},
		{65000, "1.1m"},
		{3700000, "1.0h"},
	}
	for _, tt := range tests {
		got := FormatDurationCompact(tt.ms)
		if got != tt.want {
			t.Errorf("FormatDurationCompact(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestFormatLogLines(t *testing.T) {
	messages := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: ""},
	}
	lines := FormatLogLines(messages)
	if len(lines) != 2 {
		t.Errorf("line count = %d", len(lines))
	}
	if lines[0] != "User: hello" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "Assistant: hi there" {
		t.Errorf("line 1 = %q", lines[1])
	}
}
