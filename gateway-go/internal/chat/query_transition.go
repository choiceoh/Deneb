// query_transition.go defines typed reasons for why the agent query loop
// continues or terminates. This replaces implicit string-based or boolean
// signaling with explicit, inspectable types.
//
// Inspired by Claude Code's transitions.ts pattern: separating Terminal
// (loop exited) from Continue (loop iterated) makes debugging and telemetry
// straightforward.
package chat

// TerminalReason describes why the query loop exited.
type TerminalReason string

const (
	TerminalCompleted         TerminalReason = "completed"           // normal completion
	TerminalModelError        TerminalReason = "model_error"         // LLM API error
	TerminalPromptTooLong     TerminalReason = "prompt_too_long"     // context exceeds limit
	TerminalAborted           TerminalReason = "aborted"             // user or system cancellation
	TerminalMaxTurns          TerminalReason = "max_turns"           // turn limit reached
	TerminalTimeout           TerminalReason = "timeout"             // agent timeout
	TerminalAuthError         TerminalReason = "auth_error"          // provider auth failure
	TerminalCompactionFailed  TerminalReason = "compaction_failed"   // compaction exhausted retries
	TerminalNoClient          TerminalReason = "no_client"           // no LLM client available
	TerminalBudgetExhausted   TerminalReason = "budget_exhausted"    // token budget depleted
	TerminalDiminishingReturn TerminalReason = "diminishing_returns" // budget tracker detected stall
)

// ContinueReason describes why the query loop iterated again.
type ContinueReason string

const (
	ContinueToolUse            ContinueReason = "tool_use"            // model requested tool execution
	ContinueCompactRetry       ContinueReason = "compact_retry"       // retrying after compaction
	ContinueMaxOutputRecovery  ContinueReason = "max_output_recovery" // recovering from truncated output
	ContinueBudgetContinuation ContinueReason = "budget_continuation" // continuing within token budget
	ContinueModelFallback      ContinueReason = "model_fallback"      // retrying with fallback model
)

// QueryTransition records the outcome of a single query loop iteration.
// Exactly one of Terminal or Continue is non-nil.
type QueryTransition struct {
	Terminal *TerminalReason
	Continue *ContinueReason
	Error    error // non-nil only when Terminal is set and caused by an error
}

// NewTerminal creates a terminal transition.
func NewTerminal(reason TerminalReason, err error) QueryTransition {
	return QueryTransition{Terminal: &reason, Error: err}
}

// NewContinue creates a continue transition.
func NewContinue(reason ContinueReason) QueryTransition {
	return QueryTransition{Continue: &reason}
}

// IsTerminal returns true if the transition ended the loop.
func (t QueryTransition) IsTerminal() bool {
	return t.Terminal != nil
}

// Reason returns the terminal or continue reason as a string.
func (t QueryTransition) Reason() string {
	if t.Terminal != nil {
		return string(*t.Terminal)
	}
	if t.Continue != nil {
		return string(*t.Continue)
	}
	return "unknown"
}
