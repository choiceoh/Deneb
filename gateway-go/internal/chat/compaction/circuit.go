// compaction_circuit.go implements a circuit breaker for auto-compaction.
//
// Without this, a persistent compaction failure (e.g., model always returning
// an invalid summary) causes an infinite retry loop. Claude Code observed
// "1,279 sessions had 50+ consecutive failures (up to 3,272) in a single
// session, wasting ~250K API calls/day globally."
//
// The circuit breaker disables auto-compaction after N consecutive failures,
// resetting on any success.
package compaction

import "sync"

// compactionCircuitDefaults.
const (
	// MaxConsecutiveCompactionFailures is the threshold after which
	// auto-compaction is disabled for the session.
	MaxConsecutiveCompactionFailures = 3
)

// CompactionCircuitBreaker tracks consecutive compaction failures and
// disables compaction when the threshold is reached.
type CompactionCircuitBreaker struct {
	mu                  sync.Mutex
	consecutiveFailures int
	tripped             bool
}

// NewCompactionCircuitBreaker creates a fresh circuit breaker.
func NewCompactionCircuitBreaker() *CompactionCircuitBreaker {
	return &CompactionCircuitBreaker{}
}

// RecordSuccess resets the failure counter. Call after a successful compaction.
func (cb *CompactionCircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures = 0
	cb.tripped = false
}

// RecordFailure increments the failure counter. Returns true if the circuit
// has tripped (compaction should be disabled).
func (cb *CompactionCircuitBreaker) RecordFailure() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures++
	if cb.consecutiveFailures >= MaxConsecutiveCompactionFailures {
		cb.tripped = true
	}
	return cb.tripped
}

// IsTripped returns true if compaction should be skipped.
func (cb *CompactionCircuitBreaker) IsTripped() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.tripped
}

// ConsecutiveFailures returns the current failure count.
func (cb *CompactionCircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveFailures
}

// Reset forces the circuit breaker back to the closed (healthy) state.
func (cb *CompactionCircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures = 0
	cb.tripped = false
}
