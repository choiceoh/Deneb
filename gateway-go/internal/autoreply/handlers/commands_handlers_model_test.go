// commands_handlers_model_test.go — Snapshot/text assertion tests for model,
// think, fast, verbose, reasoning, and elevated command handlers.
//
// These tests pin the exact reply text format for each action so that
// user-visible output changes are caught explicitly during refactoring.
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
	// No slash → empty provider, model = full input.
	if result.SessionMod.Provider != "" {
		t.Errorf("provider = %q, want empty", result.SessionMod.Provider)
	}
	if result.SessionMod.Model != "claude-haiku" {
		t.Errorf("model = %q, want claude-haiku", result.SessionMod.Model)
	}
}

// ── handleThinkCommand ────────────────────────────────────────────────────────

func TestHandleThinkCommand_QueryDefault(t *testing.T) {
	result, _ := handleThinkCommand(CommandContext{})
	if !strings.HasPrefix(result.Reply, "🧠 Thinking: **off**") {
		t.Errorf("reply = %q", result.Reply)
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for status query")
	}
}

func TestHandleThinkCommand_QueryShowsSessionLevel(t *testing.T) {
	result, _ := handleThinkCommand(CommandContext{
		Session: &types.SessionState{ThinkLevel: types.ThinkHigh},
	})
	if !strings.HasPrefix(result.Reply, "🧠 Thinking: **high**") {
		t.Errorf("reply = %q", result.Reply)
	}
}

func TestHandleThinkCommand_SetHigh(t *testing.T) {
	result, _ := handleThinkCommand(CommandContext{Args: &CommandArgs{Raw: "high"}})
	want := "🧠 Thinking set to: **high**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.ThinkLevel != types.ThinkHigh {
		t.Errorf("SessionMod.ThinkLevel = %q, want high", result.SessionMod.ThinkLevel)
	}
}

func TestHandleThinkCommand_SetOff(t *testing.T) {
	result, _ := handleThinkCommand(CommandContext{Args: &CommandArgs{Raw: "off"}})
	want := "🧠 Thinking set to: **off**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleThinkCommand_InvalidLevel(t *testing.T) {
	result, _ := handleThinkCommand(CommandContext{Args: &CommandArgs{Raw: "banana"}})
	if !result.IsError {
		t.Error("expected IsError=true for unknown level")
	}
	if !strings.Contains(result.Reply, "banana") {
		t.Errorf("reply should echo invalid input, got %q", result.Reply)
	}
}

// ── handleFastCommand ─────────────────────────────────────────────────────────

func TestHandleFastCommand_QueryOff(t *testing.T) {
	result, _ := handleFastCommand(CommandContext{})
	want := "⚡ Fast mode: **off**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for status query")
	}
}

func TestHandleFastCommand_QueryOn(t *testing.T) {
	result, _ := handleFastCommand(CommandContext{
		Session: &types.SessionState{FastMode: true},
	})
	want := "⚡ Fast mode: **on**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleFastCommand_SetOn(t *testing.T) {
	result, _ := handleFastCommand(CommandContext{Args: &CommandArgs{Raw: "on"}})
	want := "⚡ Fast mode: **on**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.FastMode == nil {
		t.Fatal("expected SessionMod.FastMode")
	}
	if !*result.SessionMod.FastMode {
		t.Error("expected FastMode=true")
	}
}

func TestHandleFastCommand_SetOff(t *testing.T) {
	result, _ := handleFastCommand(CommandContext{Args: &CommandArgs{Raw: "off"}})
	want := "⚡ Fast mode: **off**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.FastMode == nil {
		t.Fatal("expected SessionMod.FastMode")
	}
	if *result.SessionMod.FastMode {
		t.Error("expected FastMode=false")
	}
}

func TestHandleFastCommand_InvalidArg(t *testing.T) {
	result, _ := handleFastCommand(CommandContext{Args: &CommandArgs{Raw: "maybe"}})
	if !result.IsError {
		t.Error("expected IsError=true for invalid arg")
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
	if !strings.HasPrefix(result.Reply, "📝 Verbose: **full**") {
		t.Errorf("reply = %q", result.Reply)
	}
}

func TestHandleVerboseCommand_SetOn(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{Args: &CommandArgs{Raw: "on"}})
	want := "📝 Verbose: **on**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.VerboseLevel != types.VerboseOn {
		t.Errorf("VerboseLevel = %q, want on", result.SessionMod.VerboseLevel)
	}
}

func TestHandleVerboseCommand_SetFull(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{Args: &CommandArgs{Raw: "full"}})
	want := "📝 Verbose: **full**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.VerboseLevel != types.VerboseFull {
		t.Errorf("VerboseLevel = %q, want full", result.SessionMod.VerboseLevel)
	}
}

func TestHandleVerboseCommand_InvalidArg(t *testing.T) {
	result, _ := handleVerboseCommand(CommandContext{Args: &CommandArgs{Raw: "loud"}})
	if !result.IsError {
		t.Error("expected IsError=true for unknown level")
	}
}

// ── handleReasoningCommand ────────────────────────────────────────────────────

func TestHandleReasoningCommand_QueryDefault(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{})
	if !strings.HasPrefix(result.Reply, "💭 Reasoning: **off**") {
		t.Errorf("reply = %q", result.Reply)
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for status query")
	}
}

func TestHandleReasoningCommand_QueryShowsSessionLevel(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{
		Session: &types.SessionState{ReasoningLevel: types.ReasoningStream},
	})
	if !strings.HasPrefix(result.Reply, "💭 Reasoning: **stream**") {
		t.Errorf("reply = %q", result.Reply)
	}
}

func TestHandleReasoningCommand_SetOn(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{Args: &CommandArgs{Raw: "on"}})
	want := "💭 Reasoning: **on**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.SessionMod == nil || result.SessionMod.ReasoningLevel != types.ReasoningOn {
		t.Errorf("ReasoningLevel = %q, want on", result.SessionMod.ReasoningLevel)
	}
}

func TestHandleReasoningCommand_SetStream(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{Args: &CommandArgs{Raw: "stream"}})
	want := "💭 Reasoning: **stream**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleReasoningCommand_SetOff(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{Args: &CommandArgs{Raw: "off"}})
	want := "💭 Reasoning: **off**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleReasoningCommand_InvalidArg(t *testing.T) {
	result, _ := handleReasoningCommand(CommandContext{Args: &CommandArgs{Raw: "verbose"}})
	if !result.IsError {
		t.Error("expected IsError=true for unknown level")
	}
}

// ── handleElevatedCommand ─────────────────────────────────────────────────────

func TestHandleElevatedCommand_QueryDefault(t *testing.T) {
	result, _ := handleElevatedCommand(CommandContext{})
	if !strings.HasPrefix(result.Reply, "🔓 Elevated: **off**") {
		t.Errorf("reply = %q", result.Reply)
	}
	if result.SessionMod != nil {
		t.Error("expected no SessionMod for status query")
	}
}

func TestHandleElevatedCommand_SetOn(t *testing.T) {
	result, _ := handleElevatedCommand(CommandContext{Args: &CommandArgs{Raw: "on"}})
	want := "🔓 Elevated: **on**"
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleElevatedCommand_InvalidArg(t *testing.T) {
	result, _ := handleElevatedCommand(CommandContext{Args: &CommandArgs{Raw: "sudo"}})
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

// ── formatDurationHuman ───────────────────────────────────────────────────────

func TestFormatDurationHuman(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "disabled"},
		{-1, "disabled"},
		{30_000, "30s"},           // 30 seconds
		{90_000, "1m"},            // 1.5 min → floor to minutes
		{3_600_000, "1h"},         // 1 hour exact
		{5_400_000, "1.5h"},       // 1.5 hours
		{7_200_000, "2h"},         // 2 hours exact
		{86_400_000, "24h"},       // 1 day
	}
	for _, tt := range tests {
		got := formatDurationHuman(tt.ms)
		if got != tt.want {
			t.Errorf("formatDurationHuman(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
