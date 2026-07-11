package chat

import runstate "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"

// PendingQueue holds the latest message waiting behind an active run.
type PendingQueue = runstate.PendingQueue

// NewPendingQueue creates an empty pending-message queue.
func NewPendingQueue() *PendingQueue {
	return runstate.NewPendingQueue()
}
