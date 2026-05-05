package chat

import (
	"context"
	"testing"
)

type fakeSkillNudger struct {
	enabled bool
}

func (f fakeSkillNudger) Enabled() bool                                                { return f.enabled }
func (f fakeSkillNudger) OnToolCalls(context.Context, string, int, SkillNudgeSnapshot) {}
func (f fakeSkillNudger) Reset(string)                                                 {}

func TestShouldEnableSkillNudgerFencesAutonomousAndSelfReviewRuns(t *testing.T) {
	nudger := fakeSkillNudger{enabled: true}
	tests := []struct {
		name   string
		params RunParams
		preset string
		want   bool
	}{
		{
			name:   "normal run",
			params: RunParams{SessionKey: "telegram:1"},
			want:   true,
		},
		{
			name:   "ephemeral user",
			params: RunParams{SessionKey: "telegram:1", EphemeralUser: true},
		},
		{
			name:   "ephemeral assistant",
			params: RunParams{SessionKey: "telegram:1", EphemeralAssistant: true},
		},
		{
			name:   "self review preset",
			params: RunParams{SessionKey: "telegram:1"},
			preset: "self-review",
		},
		{
			name:   "system session",
			params: RunParams{SessionKey: "system:skill-review:telegram:1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableSkillNudger(nudger, tt.params, tt.preset)
			if got != tt.want {
				t.Fatalf("shouldEnableSkillNudger() = %v, want %v", got, tt.want)
			}
		})
	}
}
