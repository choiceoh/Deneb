package chat

import runstate "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"

// AbortTracker manages cancellation for active chat runs.
type AbortTracker = runstate.AbortTracker

// NewAbortTracker creates a tracker and starts its expiry collector.
func NewAbortTracker() *AbortTracker {
	return runstate.NewAbortTracker()
}
