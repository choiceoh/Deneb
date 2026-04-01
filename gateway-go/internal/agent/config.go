// config.go — Shared agent configuration and result types.
// Used by both chat/ and autoreply/ as the common AgentConfig contract.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// AgentConfig configures the agent execution loop.
type AgentConfig struct {
	MaxTurns  int           // Maximum tool-call turns before stopping. Default: 25.
	Timeout   time.Duration // Maximum wall time for the entire agent run. Default: 30m.
	Model     string
	System    json.RawMessage // System prompt: JSON string or array of ContentBlocks.
	Tools     []llm.Tool
	MaxTokens int // Max output tokens per LLM call. Default: 8192.

	// Sampling parameters (passed through to the LLM request).
	Temperature      *float64
	TopP             *float64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object

	// OnTurn is called after each agent turn with accumulated token count.
	// Used for mid-conversation hooks (e.g., memory extraction).
	OnTurn TurnCallback

	// OnTurnInit is called at the start of each turn to decorate the context.
	// Use this to inject per-turn state (e.g., a TurnContext for cross-tool sharing).
	// Returning nil is a no-op; returning a modified ctx replaces the turn context.
	OnTurnInit func(ctx context.Context) context.Context

	// DeferredSystemText is called before each turn starting from turn 1.
	// When it returns a non-empty string, that text is appended to System
	// once and the hook is cleared. Use this for late-arriving context
	// (e.g., proactive hints) that should be injected without blocking
	// the first turn.
	DeferredSystemText func() string

	// Thinking configures extended thinking for this run (mapped to reasoning_effort).
	// nil = disabled (default). Set via session ThinkingLevel or /think command.
	Thinking *llm.ThinkingConfig

	// StripImagesAfterFirstTurn drops base64 image data from the message history
	// after the first LLM turn. On turn 0 the image is sent normally; from turn 1
	// onward each image block is replaced with a lightweight text placeholder so
	// the bytes are not retransmitted on every subsequent turn.
	//
	// Enable when the initial message contains base64 image attachments and the
	// run is expected to take multiple turns (e.g. tool-heavy coding or analysis).
	// Savings: ~1600 tokens × image count per turn after turn 0.
	StripImagesAfterFirstTurn bool

	// NudgeBudget enables token-budget-based continuation: when the agent finishes
	// with end_turn but the token budget is not yet exhausted, a nudge message is
	// injected to prompt the LLM to check for remaining work. Nil = disabled.
	NudgeBudget *NudgeBudgetConfig

	// MaxOutputTokensRecovery is the maximum number of times to auto-recover when
	// the LLM response is truncated by max_tokens. Each recovery injects a
	// "resume where you left off" message. Default: 0 (disabled). Recommended: 3.
	MaxOutputTokensRecovery int
}

// NudgeBudgetConfig configures token-budget continuation (Claude Code pattern).
// When the agent returns end_turn with budget remaining, a nudge message is
// injected to encourage the LLM to check for remaining work.
type NudgeBudgetConfig struct {
	// MaxContinuations is the maximum number of nudge continuations. Default: 3.
	MaxContinuations int
	// BudgetThreshold is the fraction (0.0–1.0) of the total token budget at
	// which nudging stops. E.g., 0.9 means stop nudging when 90% of the budget
	// is consumed. Default: 0.9.
	BudgetThreshold float64
	// MinDeltaTokens is the minimum output tokens the LLM must produce on a
	// nudge turn to be considered productive. If the last two nudge turns both
	// produce fewer than this many tokens, nudging stops (diminishing returns).
	// Default: 500.
	MinDeltaTokens int
}

// TurnCallback is called after each agent turn with accumulated token count.
type TurnCallback func(turn int, accumulatedTokens int)

// AgentResult is the outcome of an agent run.
type AgentResult struct {
	Text       string
	StopReason string // "end_turn", "max_tokens", "timeout", "aborted", "max_turns"
	Usage      llm.TokenUsage
	Turns      int

	// InterruptedToolNames lists the tool names that were in-flight when the
	// run was aborted (context cancelled). Empty when the run completes normally.
	// Used to persist interrupted context to the transcript so the next run
	// knows what was being done when the user interrupted.
	InterruptedToolNames []string

	// NudgeContinuations is the number of token-budget nudge continuations
	// that were triggered during this run. 0 when nudge is disabled.
	NudgeContinuations int
	// MaxTokensRecoveries is the number of max-output-tokens recovery retries
	// that were triggered during this run. 0 when recovery is disabled.
	MaxTokensRecoveries int
}
