package polaris

import "sync"

// CircuitBreaker tracks consecutive condensation failures per session.
// After maxFails consecutive failures, condensation is disabled for that session.
// Any success resets the counter.
type CircuitBreaker struct {
	mu       sync.Mutex
	failures map[string]int // session_key → consecutive failure count
	maxFails int
}

// NewCircuitBreaker creates a circuit breaker with the given failure threshold.
func NewCircuitBreaker(maxFails int) *CircuitBreaker {
	if maxFails <= 0 {
		maxFails = 3
	}
	return &CircuitBreaker{
		failures: make(map[string]int),
		maxFails: maxFails,
	}
}

// Allow returns true if condensation is permitted for the session.
func (cb *CircuitBreaker) Allow(sessionKey string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures[sessionKey] < cb.maxFails
}

// RecordSuccess resets the failure counter for a session.
func (cb *CircuitBreaker) RecordSuccess(sessionKey string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.failures, sessionKey)
}

// RecordFailure increments the failure counter for a session.
func (cb *CircuitBreaker) RecordFailure(sessionKey string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures[sessionKey]++
}
