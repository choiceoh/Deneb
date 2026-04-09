// budget_tracker.go tracks token usage across continuation runs and detects
// diminishing returns, preventing wasted API calls when the agent produces
// progressively less useful output per continuation.
package chat

// BudgetTracker monitors token output across consecutive continuations.
// When 3+ continuations each produce fewer than minUsefulDelta tokens,
// the tracker signals that further continuations are unlikely to be productive.
type BudgetTracker struct {
	// minUsefulDelta is the minimum token output per continuation to be
	// considered "productive". Continuations below this threshold increment
	// the diminishing streak.
	minUsefulDelta int

	// maxBudget is the total token budget for the session.
	maxBudget int

	// totalOutput tracks cumulative output tokens across all continuations.
	totalOutput int

	// continuations is the number of continuation runs completed.
	continuations int

	// diminishingStreak counts consecutive continuations with output < minUsefulDelta.
	diminishingStreak int

	// budgetExhaustedPct is the threshold (0.0-1.0) of budget consumption
	// beyond which further continuations are stopped.
	budgetExhaustedPct float64
}

// NewBudgetTracker creates a tracker with the given total budget.
func NewBudgetTracker(maxBudget int) *BudgetTracker {
	return &BudgetTracker{
		minUsefulDelta:     500,
		maxBudget:          maxBudget,
		budgetExhaustedPct: 0.90,
	}
}

// Record logs the output tokens from a continuation run.
// Returns true if the continuation was productive (above minUsefulDelta).
func (bt *BudgetTracker) Record(outputTokens int) bool {
	bt.continuations++
	bt.totalOutput += outputTokens

	if outputTokens < bt.minUsefulDelta {
		bt.diminishingStreak++
		return false
	}
	bt.diminishingStreak = 0
	return true
}

// ShouldStop returns true if further continuations should be stopped.
// Reasons: diminishing returns (3+ low-output continuations) or budget exhaustion (>90%).
func (bt *BudgetTracker) ShouldStop() bool {
	if bt.diminishingStreak >= 3 {
		return true
	}
	if bt.maxBudget > 0 && float64(bt.totalOutput)/float64(bt.maxBudget) >= bt.budgetExhaustedPct {
		return true
	}
	return false
}

// StopReason returns a human-readable reason if ShouldStop is true.
func (bt *BudgetTracker) StopReason() string {
	if bt.diminishingStreak >= 3 {
		return "diminishing_returns"
	}
	if bt.maxBudget > 0 && float64(bt.totalOutput)/float64(bt.maxBudget) >= bt.budgetExhaustedPct {
		return "budget_exhausted"
	}
	return ""
}

// Continuations returns the number of continuation runs recorded.
func (bt *BudgetTracker) Continuations() int {
	return bt.continuations
}

// TotalOutput returns the cumulative output tokens.
func (bt *BudgetTracker) TotalOutput() int {
	return bt.totalOutput
}
