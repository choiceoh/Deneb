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

// TryReserve atomically checks whether reserve tokens are available and
// consumes them in a single CAS loop. Returns true only if the budget
// had enough room; on false the budget is unchanged.
func (b *TokenBudget) TryReserve(reserve int) bool {
	r := int64(reserve)
	for {
		cur := b.consumed.Load()
		if cur+r > b.limit {
			return false
		}
		if b.consumed.CompareAndSwap(cur, cur+r) {
			return true
		}
	}
}

// Settle adjusts the budget after a call completes. reserved is the amount
// previously claimed via TryReserve; actual is the real token usage.
// If actual < reserved, the surplus is returned to the pool.
func (b *TokenBudget) Settle(reserved, actual int) {
	diff := int64(actual) - int64(reserved)
	b.consumed.Add(diff)
}

// Consume unconditionally adds tokens to the consumed total (e.g. for
// post-hoc tracking after an LLM call completes).
// Returns true if the budget has not been exceeded after consumption.
func (b *TokenBudget) Consume(tokens int) bool {
	newTotal := b.consumed.Add(int64(tokens))
	return newTotal <= b.limit
}

// Remaining returns the approximate number of tokens left in the budget.
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
