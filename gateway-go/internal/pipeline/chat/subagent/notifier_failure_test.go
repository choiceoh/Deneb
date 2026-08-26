package subagent

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// A failed child used to produce "subagent completed … Do NOT re-do this work"
// — with NO_REPLY also forbidden, the parent's only exits were inventing a
// result or answering with nothing.
func TestFailedChildTellsTheParentToHandleItself(t *testing.T) {
	text := formatBatchNotification([]notifyItem{{
		label:         "failprobe",
		status:        session.StatusFailed,
		failureReason: "provider error",
	}})

	if strings.Contains(text, "Do NOT re-do this work") {
		t.Fatalf("must not tell the parent to skip work that never happened:\n%s", text)
	}
	if !strings.Contains(text, "직접 처리") || !strings.Contains(text, "provider error") {
		t.Fatalf("must state the failure and what to do:\n%s", text)
	}
}

func TestKilledAndTimedOutCountAsFailures(t *testing.T) {
	for _, status := range []session.RunStatus{session.StatusKilled, session.StatusTimeout} {
		text := formatBatchNotification([]notifyItem{{label: "x", status: status}})
		if strings.Contains(text, "Do NOT re-do this work") {
			t.Fatalf("%s must be treated as a failure:\n%s", status, text)
		}
	}
}

func TestAllDoneKeepsTheSynthesisInstruction(t *testing.T) {
	text := formatBatchNotification([]notifyItem{
		{label: "a", status: session.StatusDone, lastOutput: "A결과"},
		{label: "b", status: session.StatusDone, lastOutput: "B결과"},
	})

	if !strings.Contains(text, "Do NOT re-do their work") {
		t.Fatalf("a fully successful batch keeps the original instruction:\n%s", text)
	}
}
