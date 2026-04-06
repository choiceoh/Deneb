// queue_policy.go — Active run queue action resolution.
// Mirrors src/auto-reply/reply/queue-policy.ts (21 LOC).
package queue

// ActiveRunQueueAction determines what to do with an incoming message
// when a run is active.
type ActiveRunQueueAction string

const (
	QueueActionRunNow          ActiveRunQueueAction = "run-now"
	QueueActionEnqueueFollowup ActiveRunQueueAction = "enqueue-followup"
	QueueActionDrop            ActiveRunQueueAction = "drop"
)

// ResolveActiveRunQueueAction decides the queue action based on
// whether a run is active, is a heartbeat, and should follow up.
// The queue always operates in auto-debounce mode (single-user bot).
func ResolveActiveRunQueueAction(isActive, isHeartbeat, shouldFollowup bool) ActiveRunQueueAction {
	if !isActive {
		return QueueActionRunNow
	}
	if isHeartbeat {
		return QueueActionDrop
	}
	if shouldFollowup {
		return QueueActionEnqueueFollowup
	}
	return QueueActionRunNow
}
