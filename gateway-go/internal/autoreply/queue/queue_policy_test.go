package queue

import (
	"testing"
)

func TestResolveActiveRunQueueAction(t *testing.T) {
	tests := []struct {
		name           string
		isActive       bool
		isHeartbeat    bool
		shouldFollowup bool
		want           ActiveRunQueueAction
	}{
		{"not active", false, false, false, QueueActionRunNow},
		{"heartbeat dropped", true, true, false, QueueActionDrop},
		{"followup enqueued", true, false, true, QueueActionEnqueueFollowup},
		{"active run now", true, false, false, QueueActionRunNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveActiveRunQueueAction(tt.isActive, tt.isHeartbeat, tt.shouldFollowup)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
