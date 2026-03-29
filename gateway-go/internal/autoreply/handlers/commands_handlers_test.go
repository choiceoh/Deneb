package handlers

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
)

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
			ModelCandidates: []model.ModelCandidate{
				{Provider: "anthropic", Model: "claude-3", Label: "Claude 3"},
				{Provider: "openai", Model: "gpt-4", Label: "GPT-4"},
			},
		},
	})
	if !strings.Contains(result.Reply, "claude-3") {
		t.Errorf("reply should list models: %q", result.Reply)
	}
}
