package localai

import (
	"context"
	"fmt"
	"sync"
)

// tokenBudgetLimiter bounds estimated tokens across active local-AI requests.
// Waiters block on a generation channel instead of sync.Cond so caller
// cancellation and hub shutdown can interrupt admission without polling or a
// lost wakeup window.
type tokenBudgetLimiter struct {
	mu            sync.Mutex
	limit         int64
	criticalLimit int64
	inFlight      int64
	changed       chan struct{}
	closed        bool
}

func newTokenBudgetLimiter(limit int64, criticalOverdraw float64) *tokenBudgetLimiter {
	return &tokenBudgetLimiter{
		limit:         limit,
		criticalLimit: int64(float64(limit) * (1 + criticalOverdraw)),
		changed:       make(chan struct{}),
	}
}

// acquire reserves tokens, waiting until capacity is available. A request that
// exceeds its priority's entire limit is rejected immediately because no
// release could ever make it admissible.
func (l *tokenBudgetLimiter) acquire(ctx context.Context, tokens int64, priority Priority) error {
	limit := l.limit
	if priority == PriorityCritical {
		limit = l.criticalLimit
	}
	if tokens > limit {
		return fmt.Errorf("%w: estimated=%d limit=%d", ErrRequestTooLarge, tokens, limit)
	}

	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return ErrHubShutdown
		}
		if err := ctx.Err(); err != nil {
			l.mu.Unlock()
			return err
		}
		if l.inFlight+tokens <= limit {
			l.inFlight += tokens
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (l *tokenBudgetLimiter) release(tokens int64) {
	l.mu.Lock()
	l.inFlight -= tokens
	if !l.closed {
		close(l.changed)
		l.changed = make(chan struct{})
	}
	l.mu.Unlock()
}

// close rejects future admission and wakes every current waiter. Active
// reservations are released normally as their request goroutines unwind.
func (l *tokenBudgetLimiter) close() {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.changed)
	}
	l.mu.Unlock()
}
