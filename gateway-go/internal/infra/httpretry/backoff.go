package httpretry

import (
	"math"
	"math/rand/v2"
	"time"
)

// Backoff computes exponential backoff delays with optional jitter.
//
// Delay formula: min(Base * 2^(attempt-1), Max) + jitter.
// Jitter adds a random duration in [0, Jitter * delay) to prevent
// synchronized retries across concurrent clients.
type Backoff struct {
	Base   time.Duration // initial delay (attempt 1)
	Max    time.Duration // upper bound on delay before jitter
	Jitter float64       // fraction of delay added as random jitter (0 = none, 0.25 = 0-25%)
}

// Delay returns the backoff delay for the given attempt number (1-indexed).
func (b Backoff) Delay(attempt int) time.Duration {
	delay := time.Duration(float64(b.Base) * math.Pow(2, float64(attempt-1)))
	if delay > b.Max {
		delay = b.Max
	}
	if b.Jitter > 0 && delay > 0 {
		jitter := time.Duration(rand.Int64N(int64(float64(delay) * b.Jitter))) //nolint:gosec // G404 — jitter, not security
		delay += jitter
	}
	return delay
}
