package main

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Model circuits keep a failed candidate from adding the same retry delay to
// every request before a healthy fallback is tried. The state is deliberately
// model-scoped: one broken model must not sideline every model sharing an
// upstream. It is also ephemeral — a wormhole restart starts with a clean view.
const (
	circuitFailureThreshold = 2
	circuitBaseCooldown     = 15 * time.Second
	circuitMaxCooldown      = 5 * time.Minute
)

const (
	circuitClosed   = "closed"
	circuitDegraded = "degraded"
	circuitOpen     = "open"
	circuitHalfOpen = "half_open"
)

type modelCircuit struct {
	failures int
	retryAt  time.Time
}

// circuitView is the keyless operational projection exposed by GET /status.
type circuitView struct {
	State        string
	Failures     int
	RetryAfterMS int64
}

type circuitBook struct {
	mu     sync.Mutex
	models map[string]modelCircuit
	now    func() time.Time
}

func newCircuitBook() *circuitBook {
	return &circuitBook{models: map[string]modelCircuit{}, now: time.Now}
}

// order moves currently-open candidates behind candidates that are ready. Open
// candidates remain in the plan as a last resort, so the breaker is fail-open:
// it improves the normal fallback path without turning stale health state into
// a hard outage when every alternative also fails.
func (b *circuitBook) order(candidates []modelEntry) []modelEntry {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	ready := make([]modelEntry, 0, len(candidates))
	open := make([]modelEntry, 0, len(candidates))
	for _, candidate := range candidates {
		if b.viewLocked(candidate.Name, now).State == circuitOpen {
			open = append(open, candidate)
		} else {
			ready = append(ready, candidate)
		}
	}
	return append(ready, open...)
}

func (b *circuitBook) view(name string) circuitView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.viewLocked(name, b.now())
}

func (b *circuitBook) viewLocked(name string, now time.Time) circuitView {
	state, ok := b.models[name]
	if !ok || state.failures == 0 {
		return circuitView{State: circuitClosed}
	}
	if state.failures < circuitFailureThreshold {
		return circuitView{State: circuitDegraded, Failures: state.failures}
	}
	if now.Before(state.retryAt) {
		return circuitView{
			State:        circuitOpen,
			Failures:     state.failures,
			RetryAfterMS: state.retryAt.Sub(now).Milliseconds(),
		}
	}
	return circuitView{State: circuitHalfOpen, Failures: state.failures}
}

// recordFailure records one failed candidate after its within-request retries
// are exhausted. Rate limits open immediately; connection errors and 5xx need
// two consecutive failed requests. Retry-After may lengthen, but never shorten,
// the exponential cooldown.
func (b *circuitBook) recordFailure(name string, immediate bool, retryHint time.Duration) (circuitView, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	before := b.viewLocked(name, now)
	state := b.models[name]
	if immediate && state.failures < circuitFailureThreshold {
		state.failures = circuitFailureThreshold
	} else {
		state.failures++
	}
	if state.failures >= circuitFailureThreshold {
		step := state.failures - circuitFailureThreshold
		if step > 10 {
			step = 10
		}
		cooldown := circuitBaseCooldown * time.Duration(1<<step)
		if cooldown > circuitMaxCooldown {
			cooldown = circuitMaxCooldown
		}
		if retryHint > cooldown {
			cooldown = retryHint
		}
		if cooldown > circuitMaxCooldown {
			cooldown = circuitMaxCooldown
		}
		state.retryAt = now.Add(cooldown)
	}
	b.models[name] = state
	after := b.viewLocked(name, now)
	return after, before.State != circuitOpen && after.State == circuitOpen
}

// recordSuccess clears all failure history. Any non-transient HTTP response
// proves the model endpoint is reachable; auth health remains a separate signal
// owned by keyhealth.go.
func (b *circuitBook) recordSuccess(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, existed := b.models[name]
	delete(b.models, name)
	return existed
}

// circuitFailureStatus classifies only failures that another model can rescue.
// Auth and request-shape 4xx responses are deliberately excluded: key health
// and the client, respectively, own those decisions.
func circuitFailureStatus(status int) (failure, immediate bool) {
	switch {
	case status == http.StatusNotFound:
		return true, true
	case status == http.StatusTooManyRequests:
		return true, true
	case status == http.StatusRequestTimeout || status >= 500:
		return true, false
	default:
		return false, false
	}
}

// retryAfterHint parses both standard Retry-After forms: delta-seconds and an
// HTTP date. Invalid or past values are ignored.
func retryAfterHint(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}
