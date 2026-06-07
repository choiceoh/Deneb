package server

import (
	"strings"
	"testing"
)

func TestHeartbeatShouldRun(t *testing.T) {
	cases := []struct {
		name    string
		content string
		signal  string
		want    bool
	}{
		{"both empty", "", "", false},
		{"whitespace only", "   ", "  \n ", false},
		{"content only", "do X", "", true},
		{"signal only", "", "[자동 감지 신호]\n- 일정 충돌", true},
		{"both", "do X", "signal", true},
	}
	for _, c := range cases {
		if got := heartbeatShouldRun(c.content, c.signal); got != c.want {
			t.Errorf("%s: heartbeatShouldRun(%q,%q)=%v want %v", c.name, c.content, c.signal, got, c.want)
		}
	}
}

func TestComposeHeartbeatBody(t *testing.T) {
	// signal + content: signal leads, separated from HEARTBEAT.md body.
	got := composeHeartbeatBody("SIG", "MD")
	if !strings.HasPrefix(got, "SIG") || !strings.Contains(got, "---") || !strings.Contains(got, "MD") {
		t.Fatalf("both: signal must lead and content follow, got %q", got)
	}

	// signal only: includes the "no HEARTBEAT.md agenda" note.
	got = composeHeartbeatBody("SIG", "")
	if !strings.HasPrefix(got, "SIG") || !strings.Contains(got, "HEARTBEAT.md에 등록된 작업은 없습니다") {
		t.Fatalf("signal-only: want lead + empty-agenda note, got %q", got)
	}

	// content only: unchanged (no signal preamble).
	if got := composeHeartbeatBody("", "MD"); got != "MD" {
		t.Fatalf("content-only: want %q, got %q", "MD", got)
	}

	// whitespace is trimmed on both inputs.
	if got := composeHeartbeatBody("  ", "  MD  "); got != "MD" {
		t.Fatalf("whitespace trim: want %q, got %q", "MD", got)
	}
}
