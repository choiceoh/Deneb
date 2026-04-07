// commands_handlers_session_test.go — Tests for session command handlers.
package handlers

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestHandleResetCommand_ReplyText(t *testing.T) {
	result, err := handleResetCommand(CommandContext{})
	testutil.NoError(t, err)
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
