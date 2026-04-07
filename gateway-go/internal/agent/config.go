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

	// StreamingToolExecution enables dispatching tool calls as soon as each
	// tool_use block finishes streaming, rather than waiting for the entire
	// LLM response to complete. Reduces latency when the LLM emits multiple
	// tool_use blocks sequentially. Default: false (legacy behavior).
	StreamingToolExecution bool

	// NudgeBudget enables token-budget-based continuation: when the agent finishes
	// with end_turn but the token budget is not yet exhausted, a nudge message is
	// injected to prompt the LLM to check for remaining work. Nil = disabled.
	NudgeBudget *NudgeBudgetConfig

	// MaxOutputTokensRecovery is the maximum number of times to auto-recover when
	// the LLM response is truncated by max_tokens. Each recovery injects a
	// "resume where you left off" message and increases MaxTokens for the next
	// call. Default: 0 (disabled). Recommended: 3.
	MaxOutputTokensRecovery int

	// MaxOutputTokensScaleFactors controls how MaxTokens is scaled on each
	// successive max_tokens recovery. Entry [i] is the multiplier for recovery
	// attempt i+1 (1-indexed). For example, {1.5, 2.0, 2.0} means: 1st recovery
	// uses 1.5× the original MaxTokens, 2nd and 3rd use 2×.
	// When nil or shorter than the recovery attempt, defaults to 2× for missing entries.
	MaxOutputTokensScaleFactors []float64

	// ContinuationRequested returns true when the continue_run tool has been
	// called during this run. When set, the nudge budget continuation is
	// suppressed to avoid wasting turns — the explicit continuation will
	// handle follow-up work after the run completes.
	ContinuationRequested func() bool

	// SpawnDetected returns true when sessions_spawn was called during this run.
	// Used to change turn-budget warnings from "call continue_run" to
	// "end your turn — sub-agents are working", preventing the agent from
	// requesting a continuation that duplicates the sub-agents' work.
	SpawnDetected func() bool

	// DynamicToolsProvider is called before each turn starting from turn 1.
	// When it returns a non-empty slice, those tools are appended to cfg.Tools
	// (deduplicating by name). Used by the deferred tools system: fetch_tools
	// activates tools mid-run, and this hook injects their schemas into
	// subsequent LLM requests.
	DynamicToolsProvider func() []llm.Tool

	// OnMidLoopCompact is called after tool results are appended to the message
	// history on each turn. If the callback returns a non-nil replacement slice,
	// the executor swaps the message history with the compacted version. This
	// allows the chat pipeline to run microcompact / Aurora compaction mid-loop
	// instead of waiting for a context_length_exceeded error from the LLM.
	//
	// Parameters:
	//   - turn: current turn number (0-based)
	//   - messages: current message history (including latest tool results)
	//   - accTokens: accumulated input+output tokens so far
	//
	// Returns:
	//   - replacement messages (nil = no compaction needed)
	//   - optional system prompt addition from compaction summaries
	//   - error (non-fatal; logged and ignored)
	OnMidLoopCompact func(ctx context.Context, turn int, messages []llm.Message, accTokens int) ([]llm.Message, string, error)

	// OnMessagePersist is called each time a message is appended to the in-memory
	// messages array during the agent loop. The chat layer uses this to persist
	// each turn's assistant and tool_result messages to transcript immediately,
	// so intermediate findings survive across runs.
	OnMessagePersist func(msg llm.Message)

	// StreamIdleTimeout is the maximum duration to wait for the next SSE event
	// during LLM streaming. If no event arrives within this period, the stream
	// is considered stalled and aborted with a retryable error. This prevents
	// indefinite hangs when the LLM API stops sending events but keeps the TCP
	// connection alive. Default: 90s. Zero disables the watchdog.
	StreamIdleTimeout time.Duration

	// ToolLoopDetector detects stuck tool-call patterns (repeated calls, polling
	// with no progress, ping-pong oscillation). When set, the executor checks
	// each tool call against the detector and blocks execution on critical loops.
	// Nil = disabled (default).
	ToolLoopDetector *ToolLoopDetector
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

// ToolActivity records a single tool invocation during an agent run.
type ToolActivity struct {
	Name    string `json:"name"`
	IsError bool   `json:"isError,omitempty"`
}

// AgentResult is the outcome of an agent run.
type AgentResult struct {
	Text       string // last turn's text (for channel reply — avoids duplicating streamed content)
	AllText    string // accumulated text from ALL turns (for transcript persistence + session memory)
	StopReason string // "end_turn", "max_tokens", "timeout", "aborted", "max_turns"
	Usage      llm.TokenUsage
	Turns      int

	// TurnsPersisted counts messages persisted via OnMessagePersist during
	// the run. When > 0, handleRunSuccess skips aggregate transcript write.
	TurnsPersisted int

	// ToolActivities records every tool invocation in execution order.
	// Used to persist a tool activity summary alongside the assistant response
	// so subsequent runs know what the agent actually did (not just what it said).
	ToolActivities []ToolActivity

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
