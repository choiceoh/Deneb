// Package runstate defines the data passed across chat run boundaries.
package runstate

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// RunParams holds all parameters for an async agent run.
type Params struct {
	SessionKey   string
	Message      string
	Attachments  []toolport.ChatAttachment
	Model        string // role name ("main", "lightweight", "fallback"); raw model ID only via /model override
	System       string // system prompt override
	ClientRunID  string
	Delivery     *toolport.DeliveryContext
	WorkspaceDir string // per-channel workspace override (empty = use global default)

	// Sampling parameters (from OpenAI-compatible API pass-through).
	Temperature *float64
	TopP        *float64
	MaxTokens   *int // overrides default max output tokens
	MaxTurns    *int // overrides the agent-loop request count
	// MaxToolCallAttempts caps model-emitted calls across this agent run. Nil
	// is unlimited; non-nil zero is a valid hard cap.
	MaxToolCallAttempts *int
	FrequencyPenalty    *float64
	PresencePenalty     *float64
	Stop                []string
	ResponseFormat      *llm.ResponseFormat
	ToolChoice          rawJSON // "auto", "none", "required", or structured object

	// Thinking is a per-run thinking-level override: a resolveThinkingConfig
	// level ("minimal".."xhigh") or "off"/"none" to disable the thinking
	// phase. Takes precedence over the session's ThinkingLevel. Set from the
	// cron payload's `thinking` field so an operator can run a routine,
	// well-templated job without paying the dual-mode model's default
	// high-effort reasoning. Empty = session/provider default.
	Thinking string

	// InitialDeferredTools activates selected deferred tools on turn 1 for
	// runtime-owned jobs that know a named tool is mandatory. The chat layer
	// filters these names through the effective tool preset before exposing
	// schemas or seeding the deferred-activation context.
	InitialDeferredTools []string

	// PrebuiltMessages, when set, replaces the normal transcript-based context
	// assembly. Used by the OpenAI-compatible HTTP API to pass through the full
	// conversation history from the client.
	PrebuiltMessages []llm.Message

	// EphemeralUser, when true, suppresses persistence of the inbound user
	// message. Used by autonomous self-triggers (heartbeat) so the recurring
	// trigger text does not crowd out the recent-history window or bias the
	// LLM into modeling fake user requests.
	EphemeralUser bool

	// PendingEnrichment, when non-nil, is the join point of an in-flight link
	// enrichment started at the send entry (startLinkEnrichment): it blocks
	// until the URL fetches complete (bounded by their own 30s budget) and
	// returns the final message text (original + enrichment block, or the
	// original on total failure). executeAgentRun defers the user-message
	// persist to this join — running the fetches concurrently with the
	// parallel prep phase instead of serially before the run — and appends
	// the persisted bytes to the working message list itself
	// (AppendCurrentMessage), since the history was loaded before persist.
	PendingEnrichment func(ctx context.Context) string

	// AppendCurrentMessage marks that the current turn's user message is NOT
	// part of the loaded transcript history (persist deferred to the
	// enrichment join, or an ephemeral turn that never persists) and must be
	// appended to the working message list explicitly by
	// assembleTurnMessages. Set only inside executeAgentRun.
	AppendCurrentMessage bool

	// SkipRecall, when true, skips the long-term-memory recall preflight
	// (wiki/diary/transcript) for this turn — the native client's
	// "memory off / focused chat" toggle. The persona is unchanged; only the
	// work-context evidence injection is suppressed, so a general question
	// answers fast without pulling unrelated work memories. Recall is
	// tail-injected (not in the cached system prefix), so toggling it per turn
	// does not fragment the prompt cache.
	SkipRecall bool

	// FeedContext, when non-empty, is the day's-feed digest, injected as
	// wire-only context on this turn. It is what makes a chat aware of today's
	// proactive reports / captures. Set by the native bridge for recall-on
	// turns. Tail-injected alongside recall (not in the cached system prefix),
	// so it costs only its own tokens and does not fragment the prompt cache.
	FeedContext string

	// EphemeralAssistant, when true, suppresses persistence of the assistant
	// and tool_result messages produced during the run. When false, the
	// assistant's reply IS persisted — required for self-triggers that must
	// see their own prior outputs ("did I already report this 30 minutes
	// ago?") on the next iteration.
	EphemeralAssistant bool

	// AutoDeliveredOutput marks a run whose final reply text is delivered to
	// the user's channel by the run-completion layer (cron relay / main-session
	// handoff) rather than by the agent itself. Set by the cron adapter. It
	// (a) adds a Messaging directive telling the model not to deliver via the
	// `message` tool and not to report channel status, and (b) flips an
	// in-loop `message` send-guard failure from an error into a benign no-op
	// so the LLM does not translate it into a self-contradicting "channel
	// down" report delivered through that very channel.
	AutoDeliveredOutput bool

	// ToolDryRun, when true, suppresses side-effect tools for the whole run:
	// the ToolRegistry executes only its read-only allowlist (read, grep,
	// fetch_tools, read_spillover — tool_dry_run.go) and stubs everything
	// else. For eval/replay harnesses that need the real agent loop and real
	// registry without real writes/sends. Per-run only; the stubbed results
	// are persisted like any tool output, so do not combine with a session
	// you care about — pair with EphemeralUser/EphemeralAssistant.
	ToolDryRun bool

	// BeforeToolCall, when set, is consulted before each tool execution and can
	// block the call (block=true, with blockReason surfaced as the tool's error
	// output). The goal loop sets this to its idempotency guard so a re-driven
	// run cannot repeat a destructive action already committed to the goal's
	// ledger. nil = no gate. Wired in wireStreamHooks; per-run only, never
	// persisted (does not touch the transcript or the prompt cache).
	BeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)

	// OnToolResult, when set, observes each tool result (name, id, output,
	// isError). The goal loop uses it to record successfully-executed
	// destructive actions into the goal ledger — errors are skipped so a failed
	// send stays retryable. Composed (fan-out) with the broadcaster's own
	// result hook, so it never displaces streaming. nil = no observer.
	OnToolResult func(name, toolUseID, result string, isErr bool)

	// OnProgress observes deterministic server-owned phase changes for a live
	// interactive turn (preparing, recalling, thinking, finalizing, ...). The
	// callback is transport-only and never enters the model prompt or transcript.
	OnProgress func(phase string)

	// SoftDeadline is an end-to-end preference for interactive runs. Once it is
	// reached, the agent gets one no-new-tools wrap-up instruction; the caller's
	// context remains the hard cancellation boundary.
	SoftDeadline time.Duration

	// GateUntrustedTools enables the untrusted-origin tool gate for this run: if
	// a prompt-injection signature has entered the turn (flagged tool output,
	// the inbound message, or recalled memory), irreversible tools (currently
	// exec) are blocked. Set only by the interactive native-client transports.
	// Per-run, never persisted — prompt-cache neutral. See
	// untrusted_tool_gate.go.
	GateUntrustedTools bool

	// TrustedDirectUserInput is a server-owned provenance marker for a message
	// that entered through an authenticated native interactive chat surface.
	// It must never be copied from model-controlled input, session relays,
	// captures, or autonomous runs. The fact plane uses it in addition to the
	// exact home-session and promptware gates before granting mutation authority.
	TrustedDirectUserInput bool
}
