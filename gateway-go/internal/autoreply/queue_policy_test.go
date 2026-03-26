package autoreply

import (
	"testing"
)

func TestResolveActiveRunQueueAction(t *testing.T) {
	tests := []struct {
		name           string
		isActive       bool
		isHeartbeat    bool
		shouldFollowup bool
		queueMode      QueueMode
		want           ActiveRunQueueAction
	}{
		{"not active", false, false, false, QueueModeOff, QueueActionRunNow},
		{"heartbeat dropped", true, true, false, QueueModeOff, QueueActionDrop},
		{"followup enqueued", true, false, true, QueueModeOff, QueueActionEnqueueFollowup},
		{"steer enqueues", true, false, false, "steer", QueueActionEnqueueFollowup},
		{"active run now", true, false, false, QueueModeOff, QueueActionRunNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveActiveRunQueueAction(tt.isActive, tt.isHeartbeat, tt.shouldFollowup, tt.queueMode)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
