package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestNormalizeFollowupQueueMode_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  types.FollowupQueueMode
	}{
		{"", ""},
		// All recognized inputs now map to collect (single-user bot).
		{"queue", types.FollowupModeCollect},
		{"queued", types.FollowupModeCollect},
		{"steer", types.FollowupModeCollect},
		{"steering", types.FollowupModeCollect},
		{"interrupt", types.FollowupModeCollect},
		{"interrupts", types.FollowupModeCollect},
		{"abort", types.FollowupModeCollect},
		{"followup", types.FollowupModeCollect},
		{"follow-ups", types.FollowupModeCollect},
		{"followups", types.FollowupModeCollect},
		{"collect", types.FollowupModeCollect},
		{"coalesce", types.FollowupModeCollect},
		{"steer+backlog", types.FollowupModeCollect},
		{"steer-backlog", types.FollowupModeCollect},
		{"steer_backlog", types.FollowupModeCollect},
		{"STEER", types.FollowupModeCollect},
		{"  Collect  ", types.FollowupModeCollect},
		{"unknown", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeFollowupQueueMode(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeFollowupQueueMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeFollowupDropPolicy_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  types.FollowupDropPolicy
	}{
		{"", ""},
		// All recognized inputs now map to summarize (single-user bot).
		{"old", types.FollowupDropSummarize},
		{"oldest", types.FollowupDropSummarize},
		{"new", types.FollowupDropSummarize},
		{"newest", types.FollowupDropSummarize},
		{"summarize", types.FollowupDropSummarize},
		{"summary", types.FollowupDropSummarize},
		{"OLD", types.FollowupDropSummarize},
		{"  New  ", types.FollowupDropSummarize},
		{"unknown", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeFollowupDropPolicy(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeFollowupDropPolicy(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
