package autoreply

import "testing"

func TestResolveSubagentsPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/agents list", "/agents"},
		{"/subagents spawn test", "/subagents"},
		{"/agent help", "/agent"},
		{"/subagent kill 123", "/subagent"},
		{"/config set", ""},
		{"hello", ""},
		{"/agents", "/agents"},
	}
	for _, tt := range tests {
		got := ResolveSubagentsPrefix(tt.input)
		if got != tt.want {
			t.Errorf("ResolveSubagentsPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveSubagentsAction(t *testing.T) {
	tests := []struct {
		prefix string
		tokens []string
		want   SubagentsAction
	}{
		{"/agents", nil, SubagentsList},
		{"/agents", []string{"help"}, SubagentsHelp},
		{"/agents", []string{"spawn", "my-agent"}, SubagentsSpawn},
		{"/agents", []string{"kill", "123"}, SubagentsKill},
		{"/agents", []string{"list"}, SubagentsList},
		{"/agents", []string{"focus", "123"}, SubagentsFocus},
		{"/agents", []string{"unfocus"}, SubagentsUnfocus},
		{"/agents", []string{"send", "123", "hi"}, SubagentsSend},
		{"/agents", []string{"unknown"}, SubagentsHelp},
	}
	for _, tt := range tests {
		got := ResolveSubagentsAction(tt.prefix, tt.tokens)
		if got != tt.want {
			t.Errorf("ResolveSubagentsAction(%q, %v) = %q, want %q", tt.prefix, tt.tokens, got, tt.want)
		}
	}
}

func TestHandleSubagentsCommand_List(t *testing.T) {
	ctx := SubagentsCommandContext{
		RequesterKey: "test-key",
		Runs: []SubagentRun{
			{ID: "run-1", AgentID: "agent-a", Status: "running"},
		},
	}
	result := HandleSubagentsCommand("/agents list", ctx)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.ReplyText == "" {
		t.Fatal("expected reply text")
	}
}

func TestHandleSubagentsCommand_Help(t *testing.T) {
	result := HandleSubagentsCommand("/agents help", SubagentsCommandContext{})
	if result == nil {
		t.Fatal("expected result")
	}
	if result.ReplyText == "" {
		t.Fatal("expected help text")
	}
}

func TestHandleSubagentsCommand_NotSubagents(t *testing.T) {
	result := HandleSubagentsCommand("/config show", SubagentsCommandContext{})
	if result != nil {
		t.Fatal("expected nil for non-subagents command")
	}
}

func TestHandleSubagentsCommand_SpawnNoArgs(t *testing.T) {
	result := HandleSubagentsCommand("/agents spawn", SubagentsCommandContext{})
	if result == nil {
		t.Fatal("expected result")
	}
	if result.ReplyText == "" {
		t.Fatal("expected usage text")
	}
}
