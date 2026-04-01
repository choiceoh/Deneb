// budget_tracker.go implements token budget tracking with diminishing returns
// detection for the agent query loop.
//
// When the LLM is given a large token budget, it may continue generating
// content that adds little value. This tracker detects when consecutive
// continuations produce fewer and fewer tokens and signals early termination.
//
// Inspired by Claude Code's tokenBudget.ts pattern.
package chat

import "time"

// Budget tracker defaults.
const (
	// budgetCompletionThreshold is the fraction of the budget that must be
	// consumed before the tracker considers stopping.
	budgetCompletionThreshold = 0.90

	// diminishingThreshold is the minimum delta (in tokens) between
	// consecutive continuations. Below this, the continuation is considered
	// to have diminishing returns.
	diminishingThreshold = 500

	// diminishingMinContinuations is the minimum number of continuations
	// before diminishing returns detection kicks in.
	diminishingMinContinuations = 3
)

// BudgetTracker tracks token usage across query loop continuations and
// detects when further continuations yield diminishing returns.
type BudgetTracker struct {
	continuationCount  int
	lastDeltaTokens    int
	lastGlobalTokens   int
	startedAt          time.Time
}

// NewBudgetTracker creates a fresh budget tracker.
func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		startedAt: time.Now(),
	}
}

// BudgetDecision is the result of a budget check.
type BudgetDecision struct {
	// Action is "continue" or "stop".
	Action string

	// Reason explains the decision (for logging/telemetry).
	Reason string

	// ContinuationCount is how many times the loop has continued.
	ContinuationCount int

	// Pct is the percentage of budget consumed (0-100).
	Pct int

	// TurnTokens is the total tokens used in this turn.
	TurnTokens int

	// Budget is the total token budget.
	Budget int

	// DiminishingReturns is true if the stop was due to stalling output.
	DiminishingReturns bool

	// DurationMs is the elapsed time since tracking started.
	DurationMs int64
}

// CheckBudget evaluates whether the query loop should continue given the
// current token usage. Returns a decision with action "continue" or "stop".
//
// If budget is 0 or negative, the tracker is disabled and always returns "stop"
// (no continuation). Subagents (agentID != "") also skip budget tracking.
func (bt *BudgetTracker) CheckBudget(agentID string, budget int, globalTurnTokens int) BudgetDecision {
	// Disabled for subagents or when no budget is set.
	if agentID != "" || budget <= 0 {
		return BudgetDecision{Action: "stop"}
	}

	pct := 0
	if budget > 0 {
		pct = int(float64(globalTurnTokens) / float64(budget) * 100)
	}
	deltaSinceLastCheck := globalTurnTokens - bt.lastGlobalTokens

	// Diminishing returns: 3+ continuations with both the current and
	// previous delta below the threshold.
	isDiminishing := bt.continuationCount >= diminishingMinContinuations &&
		deltaSinceLastCheck < diminishingThreshold &&
		bt.lastDeltaTokens < diminishingThreshold

	// Continue if not diminishing and below 90% of budget.
	if !isDiminishing && globalTurnTokens < int(float64(budget)*budgetCompletionThreshold) {
		bt.continuationCount++
		bt.lastDeltaTokens = deltaSinceLastCheck
		bt.lastGlobalTokens = globalTurnTokens
		return BudgetDecision{
			Action:            "continue",
			Reason:            "under_budget",
			ContinuationCount: bt.continuationCount,
			Pct:               pct,
			TurnTokens:        globalTurnTokens,
			Budget:            budget,
		}
	}

	// Stop: either diminishing returns or budget threshold reached.
	if isDiminishing || bt.continuationCount > 0 {
		return BudgetDecision{
			Action:             "stop",
			Reason:             "budget_threshold_or_diminishing",
			ContinuationCount:  bt.continuationCount,
			Pct:                pct,
			TurnTokens:         globalTurnTokens,
			Budget:             budget,
			DiminishingReturns: isDiminishing,
			DurationMs:         time.Since(bt.startedAt).Milliseconds(),
		}
	}

	// First check and already over budget.
	return BudgetDecision{Action: "stop"}
}

// Reset clears the tracker state for a new turn.
func (bt *BudgetTracker) Reset() {
	bt.continuationCount = 0
	bt.lastDeltaTokens = 0
	bt.lastGlobalTokens = 0
	bt.startedAt = time.Now()
}
