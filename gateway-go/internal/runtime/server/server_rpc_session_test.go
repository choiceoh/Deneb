package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

func TestTargetedToolRunIDReturnsRunIDOnlyForWellFormedToolEvents(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		payload any
		want    string
	}{
		{
			name:    "tool event with string run id",
			event:   "session.tool",
			payload: map[string]any{"runId": "run-42"},
			want:    "run-42",
		},
		{
			name:    "non-tool event never targets",
			event:   "session.message",
			payload: map[string]any{"runId": "run-42"},
		},
		{
			name:    "non-map payload broadcasts normally",
			event:   "session.tool",
			payload: "run-42",
		},
		{
			name:    "non-string run id broadcasts normally",
			event:   "session.tool",
			payload: map[string]any{"runId": 42},
		},
		{
			name:    "empty run id broadcasts normally",
			event:   "session.tool",
			payload: map[string]any{"runId": ""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := targetedToolRunID(test.event, test.payload); got != test.want {
				t.Fatalf("targetedToolRunID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSessionInsightToolStats_PreservesCountsAndHandlesZeroCalls(t *testing.T) {
	got := sessionInsightToolStats([]agentlog.ToolStat{
		{Name: "wiki", Calls: 4, Errors: 1, AvgMs: 25},
		{Name: "unused", Calls: 0, Errors: 0, AvgMs: 0},
	})

	if len(got) != 2 {
		t.Fatalf("len(stats) = %d, want 2", len(got))
	}
	if got[0].Name != "wiki" || got[0].Calls != 4 || got[0].ErrorRate != 0.25 || got[0].AvgMs != 25 {
		t.Fatalf("first stat = %#v, want preserved wiki aggregate", got[0])
	}
	if got[1].ErrorRate != 0 {
		t.Fatalf("zero-call error rate = %v, want 0", got[1].ErrorRate)
	}
}

func TestCronRelayPresentationTruncatesPreviewAndCollapsesEmailJobs(t *testing.T) {
	if !cronJobUsesCollapsedRelay("email-single-analysis") {
		t.Fatal("email analysis must use collapsed relay")
	}
	if cronJobUsesCollapsedRelay("morning-letter") {
		t.Fatal("non-email report must keep plain relay")
	}

	short := "short analysis"
	if got := proactiveAnalysisPreview(short); got != short {
		t.Fatalf("short preview = %q, want unchanged", got)
	}
	long := strings.Repeat("x", 121)
	if got := proactiveAnalysisPreview(long); got != strings.Repeat("x", 120)+"…" {
		t.Fatalf("long preview = %q, want 120-byte head plus ellipsis", got)
	}
}
