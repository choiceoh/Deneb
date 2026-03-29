// commands_handlers_session_test.go — Snapshot/text assertion tests for
// session lifecycle command handlers (new, reset, fork, continue, send-policy).
//
// These tests pin exact reply text to catch user-visible regressions during
// autoreply module refactoring.
package handlers

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ── handleNewCommand ──────────────────────────────────────────────────────────

func TestHandleNewCommand_ReplyText(t *testing.T) {
	result, err := handleNewCommand(CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	want := "🔄 New session started."
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleNewCommand_SetsReset(t *testing.T) {
	result, _ := handleNewCommand(CommandContext{})
	if result.SessionMod == nil || !result.SessionMod.Reset {
		t.Error("expected SessionMod.Reset=true")
	}
	if !result.SkipAgent {
		t.Error("expected SkipAgent=true")
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
}

// ── handleResetCommand ────────────────────────────────────────────────────────

func TestHandleResetCommand_ReplyText(t *testing.T) {
	result, err := handleResetCommand(CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	want := "🔄 Session reset."
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
}

func TestHandleResetCommand_SetsReset(t *testing.T) {
	result, _ := handleResetCommand(CommandContext{})
	if result.SessionMod == nil || !result.SessionMod.Reset {
		t.Error("expected SessionMod.Reset=true")
	}
}

// handleNew and handleReset should produce distinct replies.
func TestHandleNewVsReset_DistinctReplies(t *testing.T) {
	newResult, _ := handleNewCommand(CommandContext{})
	resetResult, _ := handleResetCommand(CommandContext{})
	if newResult.Reply == resetResult.Reply {
		t.Errorf("handleNewCommand and handleResetCommand returned identical reply %q", newResult.Reply)
	}
}

// ── handleForkCommand ─────────────────────────────────────────────────────────

func TestHandleForkCommand_WithSession(t *testing.T) {
	result, err := handleForkCommand(CommandContext{
		Session: &types.SessionState{SessionKey: "telegram:12345"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "🍴 Session forked from `telegram:12345`."
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
}

func TestHandleForkCommand_NoSession(t *testing.T) {
	result, _ := handleForkCommand(CommandContext{})
	if !result.IsError {
		t.Error("expected IsError=true when no session")
	}
	if result.Reply == "" {
		t.Error("expected non-empty error reply")
	}
}

// ── handleContinueCommand ─────────────────────────────────────────────────────

func TestHandleContinueCommand_WithArg(t *testing.T) {
	result, err := handleContinueCommand(CommandContext{
		Args: &CommandArgs{Raw: "sess-abc-123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "▶️ Continuing session `sess-abc-123`."
	if result.Reply != want {
		t.Errorf("reply = %q, want %q", result.Reply, want)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
}

func TestHandleContinueCommand_NoArg(t *testing.T) {
	result, _ := handleContinueCommand(CommandContext{})
	if !result.IsError {
		t.Error("expected IsError=true for missing session id")
	}
	if result.Reply == "" {
		t.Error("expected non-empty error reply")
	}
}
