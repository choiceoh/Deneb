// config.go — Shared agent configuration and result types.
// Used by both chat/ and autoreply/ as the common AgentConfig contract.
package agent

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// AgentConfig configures the agent execution loop.
type AgentConfig struct {
	MaxTurns  int           // Maximum tool-call turns before stopping. Default: 25.
	Timeout   time.Duration // Maximum wall time for the entire agent run. Default: 30m.
	Model     string
	System    rawJSON // System prompt: JSON string or array of ContentBlocks.
	Tools     []llm.Tool
	MaxTokens int // Max output tokens per LLM call. Default: 8192.
	// MaxTotalOutputTokens caps the sum of output tokens across every LLM call
	// in this agent loop. When enabled, each turn is charged the greater of
	// provider usage and a deterministic estimate of its full structured output.
	// Zero keeps the production per-call-only behavior.
	MaxTotalOutputTokens int
	// MaxStreamBytes caps translated stream payload bytes across the agent run.
	// Zero keeps the production default unlimited behavior.
	MaxStreamBytes int
	// MaxToolCallAttempts caps model-emitted tool calls across the entire agent
	// run. Nil keeps production unlimited; a non-nil zero value forbids tool
	// calls while still permitting a text-only terminal response.
	MaxToolCallAttempts *int

	// Sampling parameters (passed through to the LLM request).
	Temperature      *float64
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	// Seed is forwarded only by OpenAI-compatible wire mode. Provider support
	// remains best-effort and does not make remote inference bit-identical.
	Seed           *int64
	StopSequences  []string
	ResponseFormat *llm.ResponseFormat
	ToolChoice     rawJSON // "auto", "none", "required", or structured object

	// OnTurn is called after each agent turn with accumulated token count.
	// Used for mid-conversation hooks (e.g., memory extraction).
	OnTurn TurnCallback

	// OnToolTurn is called after the per-turn ToolActivities have been
	// recorded. Used by the iteration-based skill nudger and any future
	// post-turn accounting that needs the tool-call count. Nil = disabled.
	OnToolTurn ToolTurnCallback

	// OnTurnInit is called at the start of each turn to decorate the context.
	// Use this to inject per-turn state (e.g., a TurnContext for cross-tool sharing).
	// Each call receives the run-scoped base context rather than the prior turn's
	// decorated context, so per-turn values can be released promptly. Returning
	// nil is a no-op; returning a modified ctx replaces the current turn context.
	OnTurnInit func(ctx context.Context) context.Context

	// DeferredTurnNotices is polled once per committed tool turn; each
	// returned string is appended as a trailing text block to that turn's
	// tool-results user message. This is the delivery channel for
	// late-arriving mid-run notifications (subagent completions): appending
	// them to System between turns — the previous mechanism — rewrote the
	// (system, tools) prefix mid-run, which content-prefix provider caches
	// (kimi) punish with a full cold start; a tail block on the NEW user
	// message costs only its own tokens. Undrained notices stay in the
	// caller's buffered channel and surface on the next run's drains.
	DeferredTurnNotices func() []string

	// Thinking configures extended thinking for this run (mapped to reasoning_effort).
	// nil = disabled (default). Set via session ThinkingLevel or /think command.
	Thinking *llm.ThinkingConfig

	// ThinkingModulator, when non-nil, overrides Thinking on a per-turn basis.
	// The executor calls it before building each request with the zero-based
	// turn index and THIS RUN's accumulated tool activities (o_t — name,
	// error flag, output size per call, run-scoped by construction, so
	// policies read the latest observations without re-parsing the message
	// array); a non-nil return replaces Thinking for that turn, a nil return
	// falls back to Thinking. Used by the reasoning-sandwich policy and the
	// effort router's per-step revert. Like Thinking this is a request-level
	// parameter, so varying it per turn does NOT affect prompt cache (see
	// docs/agent-rules/prompt-cache.md).
	ThinkingModulator func(turn int, toolActivities []ToolActivity) *llm.ThinkingConfig

	// ThinkingOffRetry, when non-nil, is the thinking config used to retry a turn
	// that hit max_tokens producing ONLY reasoning and no answer text (a thinking
	// runaway). The normal max_tokens recovery scales the output budget and says
	// "resume" — for a runaway that just lets the model reason even longer (dsv4
	// cannot lower reasoning_effort; it is high/max only), so a 32K-token think
	// loop becomes a 64K one. This config — carrying the model's chat_template
	// off-toggle (TemplateKwarg) — is swapped in for the one retry turn instead, so
	// the model answers directly within the normal budget. nil keeps the legacy
	// scale-and-resume recovery for every truncation. Set by the chat effort router
	// from the model capability (see applyEffortRouter).
	ThinkingOffRetry *llm.ThinkingConfig

	// FinalizeGate, when non-nil, is consulted as the model attempts to
	// finish (end_turn / no tool calls). A non-empty return blocks that
	// finish: the executor appends the assistant message, injects the
	// returned text as a user-role message, and continues the loop — the
	// same shape as the max_tokens recovery above it. The gate is expected
	// to self-limit (return "" after its injection budget) so a run can
	// always terminate. Used by the chat verification gate: runs that
	// mutated files must verify (build/test) before finishing.
	FinalizeGate func(turn int) string

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

	// DisableBudgetGrace makes MaxTurns a hard request-count ceiling. When set,
	// the executor never issues the normal extra wrap-up LLM call after the last
	// budgeted tool turn. Deterministic evaluation harnesses use this so a signed
	// turn budget cannot be exceeded by ambient production recovery behavior.
	DisableBudgetGrace bool

	// DisableTokenFeedback prevents this run from mutating the process-global
	// token estimator calibrator. Evaluation runs must not influence later arms
	// (or be influenced by their execution order) through learned global state.
	DisableTokenFeedback bool

	// DisableStreamRetry prevents replay after a mid-stream idle/error event.
	// Deterministic runs fail closed rather than duplicate a partial request.
	DisableStreamRetry bool

	// DisablePriorToolResultCompaction keeps completed turns' tool_result
	// blocks at full size instead of shrinking them mid-run
	// (CompactPriorToolResults). Set for content-prefix-cache providers
	// (kimi): the in-place history rewrite breaks their exact-prefix match,
	// so every later call re-bills the whole prompt at the cold rate — far
	// more than the compaction saves. Carried tool results are still bounded
	// per-result by the spillover cap, and run-boundary (polaris) compaction
	// is unaffected.
	DisablePriorToolResultCompaction bool
	// RequireProviderModel fails the run unless every streamed turn reports one
	// stable provider model identifier in message_start.
	RequireProviderModel bool
	// RequireExplicitStopReason rejects a text-only turn unless the provider
	// emitted an explicit, recognized terminal reason.
	RequireExplicitStopReason bool
	// RequireStrictStopShape requires tool-bearing turns to report tool_use and
	// tool-free turns to report end_turn. Deterministic evaluators use this to
	// prevent ambiguous/truncated responses from reaching tool execution.
	RequireStrictStopShape bool

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
	// connection alive. Default: 180s. Zero uses the default; a negative value
	// disables the watchdog.
	StreamIdleTimeout time.Duration

	// ToolLoopDetector detects stuck tool-call patterns (repeated calls, polling
	// with no progress, ping-pong oscillation). When set, the executor checks
	// each tool call against the detector and blocks execution on critical loops.
	// Nil = disabled (default).
	ToolLoopDetector *ToolLoopDetector

	// ParallelSafeTool vets a tool for the parallel turn path: when EVERY call
	// in a multi-tool turn is vetted and none carries $ref piping, the calls
	// execute concurrently instead of serially (see executeToolsParallelTracked for
	// the determinism staging). Only read-only tools belong here — the wiring
	// (chat's parallelSafeTools classification) is default-deny. Nil keeps
	// every turn fully sequential (default; zero behavior change).
	ParallelSafeTool func(name string) bool

	// BeforeAPICall is invoked right before each LLM request. The callback
	// may mutate the returned slice (e.g. append user-steer text to the last
	// tool_result block) and returns the adjusted messages to send. Returning
	// the input unchanged is a no-op. Nil = skipped entirely.
	//
	// The executor uses the returned slice only for the current call; its own
	// internal messages array is untouched by this hook so prompt-cache
	// stability is preserved across turns.
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
	// Turn is the 1-based turn that executed this call (the ToolTurnCallback
	// convention). Calls sharing a Turn form one batch — the unit the effort
	// router's per-step policy sizes as "the latest observation".
	Turn int `json:"turn,omitempty"`
	// OutputRunes is the rune length of the tool result content, recorded at
	// execution time so per-step consumers (ThinkingModulator) can read
	// observation size without re-parsing the message array.
	OutputRunes int `json:"outputRunes,omitempty"`
}

// StreamStats summarizes the provider-stream attempts made during an agent
// run. Attempts and Retries aggregate across turns; LastRetryReason retains
// the most recent retry trigger, while TerminationReason describes the final
// stream attempt observed by the run.
type StreamStats struct {
	Attempts          int
	Retries           int
	LastRetryReason   string
	TerminationReason string
}

// AgentResult is the outcome of an agent run.
type AgentResult struct {
	Text    string // last turn's text (for channel reply — avoids duplicating streamed content)
	AllText string // accumulated text from ALL turns (for transcript persistence + session memory)
	// DeliverableText is AllText minus the brief progress narration a model emits
	// alongside tool calls ("이제 위키 검색부터 할게요"). A turn that calls tools and
	// says only a short line is interim narration, not the answer; a terminal turn
	// (no tools) or a long content turn is the deliverable. Each kept turn also has
	// a leading self-talk preamble stripped when the report body follows behind a
	// rule/heading ("이제 분석 보고를 정리해.\n\n---\n\n## …" ships from "## …").
	// Used by proactive/cron delivery so reports don't open with the agent's
	// working narration. See the accumulation in executor_run_state.go,
	// deliverableNarrationMaxRunes, and stripNarrationHead.
	DeliverableText string
	Thinking        string // accumulated thinking text from ALL turns (interleaved + final). Empty when extended thinking is disabled.
	StopReason      string // "end_turn", "max_tokens", "timeout", "aborted", "max_turns", "max_turns_graceful"
	Usage           llm.TokenUsage
	Turns           int
	Stream          StreamStats
	ProviderModel   string

	// BudgetExhaustedInjected is true once the one-time grace-call user message
	// has been appended to the history after MaxTurns exhaustion. Guards against
	// double injection on the grace iteration itself.
	BudgetExhaustedInjected bool

	// BudgetGraceCall marks the in-flight grace iteration. Set immediately after
	// injection, cleared after the grace turn or when any result-returning exit
	// finalizes the run. Also extends the loop guard so one additional iteration
	// runs past the normal MaxTurns cap.
	BudgetGraceCall bool

	// TurnsPersisted counts messages persisted via OnMessagePersist during
	// the run. When > 0, handleRunSuccess skips aggregate transcript write.
	TurnsPersisted int

	// ToolActivities records every tool invocation in execution order.
	// Used to persist a tool activity summary alongside the assistant response
	// so subsequent runs know what the agent actually did (not just what it said).
	ToolActivities []ToolActivity

	// InterruptedToolNames lists calls that did not complete successfully when
	// a tool turn was cancelled, including calls that never started. Successful
	// calls that won the cancellation race are excluded so callers do not retry
	// already-committed side effects. Empty when the run completes normally.
	// Used to persist interruption context for the next run.
	InterruptedToolNames []string

	// MaxTokensRecoveries is the number of max-output-tokens recovery retries
	// that were triggered during this run. 0 when recovery is disabled.
	MaxTokensRecoveries int

	// LLMMs and ToolMs decompose the run's wall time into its two dominant
	// stages: provider streaming (request start → stream fully consumed,
	// including retries) and tool-turn execution (dispatch → results
	// committed). The remainder against the caller's agentMs is loop overhead
	// (prompt prep, journal, hooks). Stage attribution exists so latency
	// postmortems can tell WHERE a slow run spent its time instead of guessing
	// from the aggregate (prod-to-code: runtime signals at the granularity the
	// coding lane reasons about).
	LLMMs  int64
	ToolMs int64

	// FinalMessages is the message array at the end of the agent loop.
	FinalMessages []llm.Message

	// Run-level aggregates — surface whole-run shape for "agent loop complete"
	// log + downstream diagnostics. Without these the caller has to grep every
	// per-turn line and tally by hand. Set by the executor just before return.
	TotalTextChars int            // sum of text bytes across every turn's prose
	TotalToolCalls int            // sum of tool_use blocks across every turn
	ToolCounts     map[string]int // histogram of tool name -> invocation count
}
