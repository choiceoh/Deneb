package chat

import runstate "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"

// abortTracker manages cancellation for active chat runs.
type abortTracker = runstate.AbortTracker

// newAbortTracker creates a tracker and starts its expiry collector.
func newAbortTracker() *abortTracker {
	return runstate.NewAbortTracker()
}
