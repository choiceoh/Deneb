package chat

import "time"

const (
	proactiveTargetReadyBudget = 3 * time.Second
	proactiveMinGraceWait      = 120 * time.Millisecond
	proactiveMaxGraceWait      = 1500 * time.Millisecond
)

// computeProactiveGraceWait returns how much extra time executeAgentRun should
// wait for the proactive hint after the parallel prep section completes.
//
// The proactive worker already runs during prep, so we aim for a total budget
// from launch rather than always adding a fixed wait. This improves hit rate
// without turning proactive context back into a multi-second blocking step.
func computeProactiveGraceWait(elapsed time.Duration) time.Duration {
	wait := proactiveTargetReadyBudget - elapsed
	if wait < proactiveMinGraceWait {
		return proactiveMinGraceWait
	}
	if wait > proactiveMaxGraceWait {
		return proactiveMaxGraceWait
	}
	return wait
}
