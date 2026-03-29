// commands_handlers_model_test.go — Tests for model and verbose command handlers.
package handlers

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ── handleModelCommand ────────────────────────────────────────────────────────

func TestHandleModelCommand_NoArg_NoSession(t *testing.T) {
	result, err := handleModelCommand(CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "Usage: /model <provider/model>" {
		t.Errorf("reply = %q", result.Reply)
	}
	if !result.SkipAgent {
		t.Error("expected SkipAgent=true")
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for no-arg query")
	}
}

func TestHandleModelCommand_NoArg_ShowsCurrentModel(t *testing.T) {
	result, _ := handleModelCommand(CommandContext{
		Session: &types.SessionState{Provider: "anthropic", Model: "claude-opus-4"},
	})
	if !strings.HasPrefix(result.Reply, "🤖 Current model:") {
		t.Errorf("missing prefix, reply = %q", result.Reply)
	}
	if !strings.Contains(result.Reply, "anthropic/claude-opus-4") {
		t.Errorf("missing model ref, reply = %q", result.Reply)
	}
}

func TestHandleModelCommand_SetWithSlash(t *testing.T) {
	result, err := handleModelCommand(CommandContext{
		Args: &CommandArgs{Raw: "anthropic/claude-sonnet"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Reply, "🤖 Model set to:") {
		t.Errorf("missing prefix, reply = %q", result.Reply)
	}
	if !strings.Contains(result.Reply, "anthropic/claude-sonnet") {
		t.Errorf("missing model ref, reply = %q", result.Reply)
	}
	if result.SessionMod == nil {
		t.Fatal("expected SessionMod")
	}
	if result.SessionMod.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", result.SessionMod.Provider)
	}
	if result.SessionMod.Model != "claude-sonnet" {
		t.Errorf("model = %q, want claude-sonnet", result.SessionMod.Model)
	}
}

func TestHandleModelCommand_SetNoProvider(t *testing.T) {
	result, _ := handleModelCommand(CommandContext{
		Args: &CommandArgs{Raw: "claude-haiku"},
	})
	if result.SessionMod == nil {
		t.Fatal("expected SessionMod")
	}
	if result.SessionMod.Provider != "" {
		t.Errorf("provider = %q, want empty", result.SessionMod.Provider)
	}
	if result.SessionMod.Model != "claude-haiku" {
		t.Errorf("model = %q, want claude-haiku", result.SessionMod.Model)
	}
}

// ── handleVerboseCommand ──────────────────────────────────────────────────────

func TestHandleVerboseCommand_QueryDefault(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{})
	if !strings.HasPrefix(result.Reply, "📝 Verbose: **off**") {
		t.Errorf("reply = %q", result.Reply)
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for status query")
	}
}

func TestHandleVerboseCommand_QueryShowsSessionLevel(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{
		Session: &types.SessionState{VerboseLevel: types.VerboseFull},
	})
	if !strings.Contains(result.Reply, "full") {
		t.Errorf("reply = %q", result.Reply)
	}
}

func TestHandleVerboseCommand_SetOn(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{
		Args: &CommandArgs{Raw: "on"},
	})
	if result.SessionMod == nil || result.SessionMod.VerboseLevel != types.VerboseOn {
		t.Errorf("expected VerboseLevel=on, got %+v", result.SessionMod)
	}
	if result.IsError {
		t.Error("expected no error")
	}
}

func TestHandleVerboseCommand_SetFull(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{
		Args: &CommandArgs{Raw: "full"},
	})
	if result.SessionMod == nil || result.SessionMod.VerboseLevel != types.VerboseFull {
		t.Errorf("expected VerboseLevel=full, got %+v", result.SessionMod)
	}
}

func TestHandleVerboseCommand_InvalidArg(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{
		Args: &CommandArgs{Raw: "gibberish"},
	})
	if !result.IsError {
		t.Error("expected IsError=true for invalid arg")
	}
}
