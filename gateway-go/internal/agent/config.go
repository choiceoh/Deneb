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
}
