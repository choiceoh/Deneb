package rlm

import "sync/atomic"

// TokenBudget tracks cumulative token usage across main + sub-LLM calls.
// Safe for concurrent use from multiple goroutines.
type TokenBudget struct {
	limit    int64
	consumed atomic.Int64
}

// NewTokenBudget creates a budget with the given token limit.
func NewTokenBudget(limit int) *TokenBudget {
	return &TokenBudget{limit: int64(limit)}
}

// Consume attempts to consume tokens from the budget.
// Returns true if the budget has not been exceeded after consumption.
func (b *TokenBudget) Consume(tokens int) bool {
	newTotal := b.consumed.Add(int64(tokens))
	return newTotal <= b.limit
}

// Remaining returns the number of tokens left in the budget.
func (b *TokenBudget) Remaining() int {
	r := b.limit - b.consumed.Load()
	if r < 0 {
		return 0
	}
	return int(r)
}

// Used returns the total tokens consumed so far.
func (b *TokenBudget) Used() int {
	return int(b.consumed.Load())
}
