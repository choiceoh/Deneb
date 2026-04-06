package protocol_test

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestParseSessionKind(t *testing.T) {
	tests := []struct {
		input string
		want  protocol.SessionKind
	}{
		{"direct", protocol.SessionKindDirect},
		{"group", protocol.SessionKindGroup},
		{"global", protocol.SessionKindGlobal},
		{"unknown", protocol.SessionKindUnknown},
		{"cron", protocol.SessionKindCron},
		{"subagent", protocol.SessionKindSubagent},
		{"shadow", protocol.SessionKindDirect}, // shadow removed; falls through to default
		{"", protocol.SessionKindDirect},
		{"bogus", protocol.SessionKindDirect},
	}
	for _, tc := range tests {
		got := protocol.ParseSessionKind(tc.input)
		if got != tc.want {
			t.Errorf("ParseSessionKind(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSessionStatusConstants(t *testing.T) {
	// Verify wire values match TypeScript conventions.
	if protocol.SessionStatusRunning != "running" {
		t.Errorf("SessionStatusRunning = %q, want %q", protocol.SessionStatusRunning, "running")
	}
	if protocol.SessionStatusDone != "done" {
		t.Errorf("SessionStatusDone = %q, want %q", protocol.SessionStatusDone, "done")
	}
}
