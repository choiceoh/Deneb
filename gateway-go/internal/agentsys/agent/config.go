// config.go — Shared agent configuration and result types.
// Used by both chat/ and autoreply/ as the common AgentConfig contract.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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

	// OnToolTurn is called after the per-turn ToolActivities have been
	// recorded. Used by the iteration-based skill nudger and any future
	// post-turn accounting that needs the tool-call count. Nil = disabled.
	OnToolTurn ToolTurnCallback

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

	// SpawnDetected returns true when sessions_spawn was called during this run.
	// Used to emit a turn-budget warning that tells the agent to wrap up and
	// yield to the sub-agent notification system when the turn limit is near.
	SpawnDetected func() bool

	// DynamicToolsProvider is called before each turn starting from turn 1.
	// When it returns a non-empty slice, those tools are appended to cfg.Tools
	// (deduplicating by name). Used by the deferred tools system: fetch_tools
	// activates tools mid-run, and this hook injects their schemas into
	// subsequent LLM requests.
	DynamicToolsProvider func() []llm.Tool

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

	// BeforeAPICall is invoked right before each LLM request. The callback
	// may mutate the returned slice (e.g. append user-steer text to the last
	// tool_result block) and returns the adjusted messages to send. Returning
	// the input unchanged is a no-op. Nil = skipped entirely.
	//
	// The executor re-reads cfg.Messages from the return value only for the
	// current call; its own internal messages array is untouched by this
	// hook so prompt-cache stability is preserved across turns.
	//
	// Single writer: callers that need multiple hooks must compose them
	// explicitly via ComposeBeforeAPICall. Overwriting this field silently
	// replaces any prior hook — prefer composing.
	BeforeAPICall func(messages []llm.Message) []llm.Message
}

// ComposeBeforeAPICall returns a single BeforeAPICall hook that threads the
// messages slice through each supplied hook in order. nil entries are skipped
// (a common shape when features are conditionally enabled). Returns nil when
// no non-nil hooks are supplied so callers can unconditionally assign the
// result without worrying about wrapping an empty chain.
//
// Example:
//
//	cfg.BeforeAPICall = agent.ComposeBeforeAPICall(
//	    buildSteerBeforeAPICall(...),
//	    buildMetricsBeforeAPICall(...),
//	)
func ComposeBeforeAPICall(hooks ...func(messages []llm.Message) []llm.Message) func(messages []llm.Message) []llm.Message {
	nonNil := make([]func([]llm.Message) []llm.Message, 0, len(hooks))
	for _, h := range hooks {
		if h != nil {
			nonNil = append(nonNil, h)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return func(msgs []llm.Message) []llm.Message {
			for _, h := range nonNil {
				msgs = h(msgs)
			}
			return msgs
		}
	}
}

// TurnCallback is called after each agent turn with accumulated token count.
type TurnCallback func(turn int, accumulatedTokens int)

// ToolTurnCallback is invoked after every turn whose tool executions
// have been recorded into result.ToolActivities. It receives the
// 1-based turn index and the per-turn tool activities (name + error
// flag) in the order they executed. Called even when the turn had no
// tool calls (empty slice) so subscribers can track turn progression.
// Runs synchronously on the executor goroutine; the callback must
// return quickly and delegate any long work to a background goroutine.
type ToolTurnCallback func(turn int, activities []ToolActivity)

// ToolActivity records a single tool invocation during an agent run.
type ToolActivity struct {
	Name    string `json:"name"`
	IsError bool   `json:"isError,omitempty"`
}

// AgentResult is the outcome of an agent run.
type AgentResult struct {
	Text       string // last turn's text (for channel reply — avoids duplicating streamed content)
	AllText    string // accumulated text from ALL turns (for transcript persistence + session memory)
	Thinking   string // accumulated thinking text from ALL turns (interleaved + final). Empty when extended thinking is disabled.
	StopReason string // "end_turn", "max_tokens", "timeout", "aborted", "max_turns", "max_turns_graceful"
	Usage      llm.TokenUsage
	Turns      int

	// BudgetExhaustedInjected is true once the one-time grace-call user message
	// has been appended to the history after MaxTurns exhaustion. Guards against
	// double injection on the grace iteration itself.
	BudgetExhaustedInjected bool

	// BudgetGraceCall marks the in-flight grace iteration. Set immediately after
	// injection, cleared after the grace turn finishes. Also extends the loop
	// guard so one additional iteration runs past the normal MaxTurns cap.
	BudgetGraceCall bool

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

	// MaxTokensRecoveries is the number of max-output-tokens recovery retries
	// that were triggered during this run. 0 when recovery is disabled.
	MaxTokensRecoveries int

	// FinalMessages is the message array at the end of the agent loop.
	FinalMessages []llm.Message

	// Run-level aggregates — surface whole-run shape for "agent loop complete"
	// log + downstream diagnostics. Without these the caller has to grep every
	// per-turn line and tally by hand. Set by the executor just before return.
	TotalTextChars int            // sum of text bytes across every turn's prose
	TotalToolCalls int            // sum of tool_use blocks across every turn
	ToolCounts     map[string]int // histogram of tool name -> invocation count
}
