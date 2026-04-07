package handlers

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestCommandRouter_DispatchModel(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	result, err := r.Dispatch(CommandContext{
		Command: "model",
		Args:    &CommandArgs{Raw: "anthropic/claude-3"},
	})
	testutil.NoError(t, err)
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

func TestCommandRouter_UnknownCommand(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	_, err := r.Dispatch(CommandContext{Command: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}
