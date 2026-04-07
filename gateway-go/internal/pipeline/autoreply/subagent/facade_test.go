package subagent

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

func TestIsACPSessionFacade(t *testing.T) {
	if !IsACPSession("acp:abc") {
		t.Fatalf("expected ACP session prefix to be recognized")
	}
	if IsACPSession("telegram:abc") {
		t.Fatalf("unexpected ACP recognition for non-ACP session")
	}
}

func TestStopReasonFacade(t *testing.T) {
	if got := TranslateStopReason(session.StatusDone); got != "stop" {
		t.Fatalf("expected stop reason stop, got %q", got)
	}
	if got := TranslateACPStopReasonToStatus("cancel"); got != session.StatusKilled {
		t.Fatalf("expected killed status for cancel, got %q", got)
	}
}
