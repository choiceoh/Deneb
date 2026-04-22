package polaris

import (
	"sync"
	"time"
)

// summaryFailureCooldown is the time window during which condensation is
// suppressed for a session after hitting the failure threshold. After the
// window elapses, the breaker auto-resets so sustained outages recover
// without needing an explicit success signal (which may never come while
// condensation is disabled).
//
// Value borrowed from NousResearch/hermes-agent
// (context_compressor._SUMMARY_FAILURE_COOLDOWN_SECONDS = 600).
const summaryFailureCooldown = 10 * time.Minute

// CircuitBreaker tracks consecutive condensation failures per session.
// After maxFails consecutive failures, condensation is disabled for the
// session until either (a) a success is recorded or (b) the cooldown window
// elapses — whichever comes first.
type CircuitBreaker struct {
	mu       sync.Mutex
	state    map[string]*breakerState
	maxFails int
}

type breakerState struct {
	failures int
	openedAt time.Time // zero when not tripped
}

// NewCircuitBreaker creates a circuit breaker with the given failure threshold.
func NewCircuitBreaker(maxFails int) *CircuitBreaker {
	if maxFails <= 0 {
		maxFails = 3
	}
	return &CircuitBreaker{
		state:    make(map[string]*breakerState),
		maxFails: maxFails,
	}
}

// Allow returns true if condensation is permitted for the session.
// When the breaker is tripped we check whether the cooldown has elapsed; if
// so we reset the session and allow a fresh attempt.
func (cb *CircuitBreaker) Allow(sessionKey string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	s, ok := cb.state[sessionKey]
	if !ok {
		return true
	}
	if s.failures < cb.maxFails {
		return true
	}
	// Tripped — check cooldown.
	if !s.openedAt.IsZero() && time.Since(s.openedAt) >= summaryFailureCooldown {
		delete(cb.state, sessionKey)
		return true
	}
	return false
}

// RecordSuccess resets the failure counter for a session.
func (cb *CircuitBreaker) RecordSuccess(sessionKey string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.state, sessionKey)
}

// RecordFailure increments the failure counter for a session.
// When the counter hits maxFails we stamp openedAt so the cooldown timer
// starts. Additional failures after tripping don't re-stamp.
func (cb *CircuitBreaker) RecordFailure(sessionKey string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	s, ok := cb.state[sessionKey]
	if !ok {
		s = &breakerState{}
		cb.state[sessionKey] = s
	}
	s.failures++
	if s.failures >= cb.maxFails && s.openedAt.IsZero() {
		s.openedAt = time.Now()
	}
}
