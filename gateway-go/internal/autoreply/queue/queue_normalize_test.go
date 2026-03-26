package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"testing"
)

func TestNormalizeFollowupQueueMode_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  types.FollowupQueueMode
	}{
		{"", ""},
		{"queue", types.FollowupModeSteer},
		{"queued", types.FollowupModeSteer},
		{"steer", types.FollowupModeSteer},
		{"steering", types.FollowupModeSteer},
		{"interrupt", types.FollowupModeInterrupt},
		{"interrupts", types.FollowupModeInterrupt},
		{"abort", types.FollowupModeInterrupt},
		{"followup", types.FollowupModeFollowup},
		{"follow-ups", types.FollowupModeFollowup},
		{"followups", types.FollowupModeFollowup},
		{"collect", types.FollowupModeCollect},
		{"coalesce", types.FollowupModeCollect},
		{"steer+backlog", types.FollowupModeSteerBacklog},
		{"steer-backlog", types.FollowupModeSteerBacklog},
		{"steer_backlog", types.FollowupModeSteerBacklog},
		{"STEER", types.FollowupModeSteer},
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
		{"old", types.FollowupDropOld},
		{"oldest", types.FollowupDropOld},
		{"new", types.FollowupDropNew},
		{"newest", types.FollowupDropNew},
		{"summarize", types.FollowupDropSummarize},
		{"summary", types.FollowupDropSummarize},
		{"OLD", types.FollowupDropOld},
		{"  New  ", types.FollowupDropNew},
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
