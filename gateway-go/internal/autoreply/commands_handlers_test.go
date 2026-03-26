package autoreply

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func testSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCommandRouter_DispatchNew(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	result, err := r.Dispatch(CommandContext{Command: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SkipAgent {
		t.Error("expected SkipAgent")
	}
	if result.SessionMod == nil || !result.SessionMod.Reset {
		t.Error("expected session reset")
	}
}

func TestCommandRouter_DispatchModel(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	result, err := r.Dispatch(CommandContext{
		Command: "model",
		Args:    &CommandArgs{Raw: "anthropic/claude-3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionMod == nil {
		t.Fatal("expected session mod")
	}
	if result.SessionMod.Model != "claude-3" {
		t.Errorf("model = %q", result.SessionMod.Model)
	}
	if result.SessionMod.Provider != "anthropic" {
		t.Errorf("provider = %q", result.SessionMod.Provider)
	}
}

func TestCommandRouter_DispatchThink(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	// With arg.
	result, _ := r.Dispatch(CommandContext{Command: "think", Args: &CommandArgs{Raw: "high"}})
	if result.SessionMod.ThinkLevel != ThinkHigh {
		t.Errorf("think = %q, want high", result.SessionMod.ThinkLevel)
	}

	// Without arg (show current).
	result, _ = r.Dispatch(CommandContext{
		Command: "think",
		Session: &SessionState{ThinkLevel: ThinkMedium},
	})
	if !strings.Contains(result.Reply, "medium") {
		t.Errorf("reply should show current level: %q", result.Reply)
	}

	// Invalid arg.
	result, _ = r.Dispatch(CommandContext{Command: "think", Args: &CommandArgs{Raw: "banana"}})
	if !result.IsError {
		t.Error("expected error for invalid level")
	}
}

func TestCommandRouter_DispatchFast(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{Command: "fast", Args: &CommandArgs{Raw: "on"}})
	if result.SessionMod == nil || result.SessionMod.FastMode == nil || !*result.SessionMod.FastMode {
		t.Error("expected fast mode on")
	}

	result, _ = r.Dispatch(CommandContext{Command: "fast", Args: &CommandArgs{Raw: "off"}})
	if result.SessionMod == nil || result.SessionMod.FastMode == nil || *result.SessionMod.FastMode {
		t.Error("expected fast mode off")
	}

	// Status query.
	result, _ = r.Dispatch(CommandContext{
		Command: "fast",
		Session: &SessionState{FastMode: true},
	})
	if !strings.Contains(result.Reply, "on") {
		t.Errorf("reply should show 'on': %q", result.Reply)
	}
}

func TestCommandRouter_DispatchBash_Blocked(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{
		Command: "bash",
		Args:    &CommandArgs{Raw: "ls"},
		Session: &SessionState{ElevatedLevel: ElevatedOff},
	})
	if !result.IsError {
		t.Error("expected error when elevated is off")
	}
}

func TestCommandRouter_DispatchApprove(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{
		Command: "approve",
		Args:    &CommandArgs{Raw: "abc123 always"},
	})
	if !strings.Contains(result.Reply, "always") {
		t.Errorf("reply should contain decision: %q", result.Reply)
	}
}

func TestCommandRouter_DispatchActivation(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{Command: "activation", Args: &CommandArgs{Raw: "always"}})
	if result.SessionMod.GroupActivation != ActivationAlways {
		t.Error("expected always activation")
	}

	result, _ = r.Dispatch(CommandContext{Command: "activation", Args: &CommandArgs{Raw: "invalid"}})
	if !result.IsError {
		t.Error("expected error for invalid activation")
	}
}

func TestCommandRouter_DispatchSession(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{
		Command: "session",
		Args:    &CommandArgs{Raw: "idle 2h"},
	})
	if result.SessionMod == nil || result.SessionMod.IdleTimeoutMs != 7200000 {
		t.Errorf("expected 7200000ms, got %v", result.SessionMod)
	}

	result, _ = r.Dispatch(CommandContext{
		Command: "session",
		Args:    &CommandArgs{Raw: "idle off"},
	})
	if result.SessionMod == nil || result.SessionMod.IdleTimeoutMs != 0 {
		t.Error("expected 0 (disabled)")
	}
}

func TestCommandRouter_DispatchAllowlist(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))

	result, _ := r.Dispatch(CommandContext{
		Command: "allowlist",
		Args:    &CommandArgs{Raw: "add @user123"},
	})
	if !strings.Contains(result.Reply, "user123") {
		t.Errorf("reply should mention user: %q", result.Reply)
	}
}

func TestCommandRouter_UnknownCommand(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	_, err := r.Dispatch(CommandContext{Command: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestCommandRouter_ModelsWithPagination(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	result, _ := r.Dispatch(CommandContext{
		Command: "models",
		Deps: &CommandDeps{
			ModelCandidates: []ModelCandidate{
				{Provider: "anthropic", Model: "claude-3", Label: "Claude 3"},
				{Provider: "openai", Model: "gpt-4", Label: "GPT-4"},
			},
		},
	})
	if !strings.Contains(result.Reply, "claude-3") {
		t.Errorf("reply should list models: %q", result.Reply)
	}
}

func TestParseSessionDuration(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"2h", 7200000, false},
		{"30m", 1800000, false},
		{"off", 0, false},
		{"disable", 0, false},
		{"24", 86400000, false}, // bare number = hours
		{"", 0, true},
		{"invalid", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSessionDuration(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseSessionDuration(%q) expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseSessionDuration(%q) unexpected error: %v", tt.input, err)
		}
		if !tt.err && got != tt.want {
			t.Errorf("parseSessionDuration(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
