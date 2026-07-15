package chat

import runstate "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"

// pendingQueue holds the latest message waiting behind an active run.
type pendingQueue = runstate.PendingQueue

// newPendingQueue creates an empty pending-message queue.
func newPendingQueue() *pendingQueue {
	return runstate.NewPendingQueue()
}
