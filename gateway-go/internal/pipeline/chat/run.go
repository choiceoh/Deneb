package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// cachedWorkspaceDir caches the resolved workspace directory at startup
// to avoid disk I/O (config.LoadConfigFromDefaultPath) on every chat message.
// Single-user deployment: config doesn't change at runtime.
var (
	cachedWorkspaceDir     string
	cachedWorkspaceDirOnce sync.Once
)

// RunParams holds all parameters for an async agent run.
type RunParams struct {
	SessionKey   string
	Message      string
	Attachments  []ChatAttachment
	Model        string // role name ("main", "lightweight", "fallback"); raw model ID only via /model override
	System       string // system prompt override
	ClientRunID  string
	Delivery     *DeliveryContext
	WorkspaceDir string // per-channel workspace override (empty = use global default)

	// Sampling parameters (from OpenAI-compatible API pass-through).
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int // overrides default max output tokens
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Stop             []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object

	// Thinking is a per-run thinking-level override: a resolveThinkingConfig
	// level ("minimal".."xhigh") or "off"/"none" to disable the thinking
	// phase. Takes precedence over the session's ThinkingLevel. Set from the
	// cron payload's `thinking` field so an operator can run a routine,
	// well-templated job without paying the dual-mode model's default
	// high-effort reasoning. Empty = session/provider default.
	Thinking string

	// PrebuiltMessages, when set, replaces the normal transcript-based context
	// assembly. Used by the OpenAI-compatible HTTP API to pass through the full
	// conversation history from the client.
	PrebuiltMessages []llm.Message

	// EphemeralUser, when true, suppresses persistence of the inbound user
	// message. Used by autonomous self-triggers (heartbeat) so the recurring
	// trigger text does not crowd out the recent-history window or bias the
	// LLM into modeling fake user requests.
	EphemeralUser bool

	// SkipRecall, when true, skips the long-term-memory recall preflight
	// (wiki/diary/transcript) for this turn — the native client's
	// "memory off / focused chat" toggle. The persona is unchanged; only the
	// work-context evidence injection is suppressed, so a general question
	// answers fast without pulling unrelated work memories. Recall is
	// tail-injected (not in the cached system prefix), so toggling it per turn
	// does not fragment the prompt cache.
	SkipRecall bool

	// FeedContext, when non-empty, is the 업무 (work) workspace's day's-feed
	// digest, injected as wire-only context on this turn. It is what makes a 업무
	// chat aware of today's proactive reports / captures, versus a context-less
	// 챗봇 chat — the functional difference between the two modes beyond recall.
	// Set by the native bridge only for 업무 turns (recall on). Tail-injected
	// alongside recall (not in the cached system prefix), so it costs only its
	// own tokens and does not fragment the prompt cache.
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

	// GateUntrustedTools enables the untrusted-origin tool gate for this run: if
	// a prompt-injection signature has entered the turn (flagged tool output,
	// the inbound message, or recalled memory), irreversible tools (exec, gmail
	// send/reply) are blocked. Set only by the interactive native-client
	// transports. Per-run, never persisted — prompt-cache neutral. See
	// untrusted_tool_gate.go.
	GateUntrustedTools bool
}

// Agent run defaults.
const (
	defaultMaxTokens    = 32768
	defaultMaxTurns     = 50
	defaultAgentTimeout = 60 * time.Minute
)

// chatportAdapters holds injected implementations that decouple chat from autoreply.
// When nil, the corresponding functionality is simply skipped.
//
// Transient HTTP retry classification used to live here via an injected
// IsTransientError func; it now goes through the shared pkg/llmerr classifier
// (see isTransientLLMError in run_helpers.go) so there is no plugin point.
type chatportAdapters struct {
	NewTypingSignaler    func(onStart func()) chatport.TypingSignaler // optional; creates phase-aware typing signaler
	SanitizeDraft        chatport.DraftSanitizerFunc                  // optional; cleans streaming draft text
	ParseReplyDirectives chatport.ParseReplyDirectivesFunc            // optional; parses reply directives
}

// runDeps holds the dependencies the async run needs from the Handler.
// Optional fields (may be nil): transcript, tools, authManager,
// broadcast, jobTracker. Required: sessions, logger.
type runDeps struct {
	sessions        *session.Manager                  // required
	llmClient       *llm.Client                       // optional; resolved from authManager if nil
	transcript      TranscriptStore                   // optional; history unavailable without it
	tools           *ToolRegistry                     // optional; no tool use if nil
	authManager     *provider.AuthManager             // optional; uses pre-configured client if nil
	providerRuntime *provider.ProviderRuntimeResolver // optional; runtime auth, missing-auth messages
	broadcast       BroadcastFunc                     // optional
	jobTracker      *agent.JobTracker                 // optional
	// channelUploadLimitFn returns the max file upload size for a channel ID.
	// Returns 0 if no limit is registered (tool applies its own default).
	channelUploadLimitFn func(channelID string) int64 // optional
	providerConfigs      map[string]ProviderConfig    // optional; config-based provider credentials
	logger               *slog.Logger                 // required (defaults to slog.Default)

	embeddingClient      compact.Embedder          // optional; BGE-M3 for MMR compaction fallback
	wikiStore            *wiki.Store               // optional; wiki knowledge base
	notebookStore        *notebook.Store           // optional; notebook session-grounding source store
	dreamTurnFn          func(ctx context.Context) // optional; increments dream turn via autonomous
	agentLog             *agentlog.Writer          // optional; enables agent detail logging
	registry             *modelrole.Registry       // centralized model role registry
	contextCfg           ContextConfig
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int
	// drainPendingFn drains the next queued message for a session after the
	// current run completes. Set by the Handler; nil disables pending queue.
	drainPendingFn func(sessionKey string) *RunParams
	// startRunFn starts a new async run (for processing queued messages).
	// Set by the Handler; nil disables pending queue processing.
	startRunFn func(params RunParams)

	// subagentNotifyCh receives completion notifications for child sessions
	// spawned by the current session. Consumed by DeferredSystemText to inject
	// notifications mid-run without polling. nil if not applicable.
	subagentNotifyCh <-chan string

	// steerQueue is the per-Handler /steer note queue. The agent run goroutine
	// drains it via BeforeAPICall to inject notes into the next tool_result.
	// nil disables the mid-run steer feature.
	steerQueue *SteerQueue

	// skillNudger fires mid-session skill reviews every N tool calls.
	// nil disables iteration-based nudging (session-end genesis still runs).
	skillNudger SkillNudger

	// skillUsageRecorder records, per turn, which skills were consulted and
	// whether the turn succeeded — feeds the genesis Evolver's success-rate
	// gate. nil disables usage attribution.
	skillUsageRecorder SkillUsageRecorder

	// callbacks is an atomic snapshot of channel callbacks taken at run start.
	// Contains reply, media, typing, reaction, draft, emit, shutdown, and model fields.
	callbacks CallbackSnapshot

	// topicResolver maps a forum threadID to a per-topic knowledge key for
	// system-prompt injection. nil disables per-topic knowledge.
	topicResolver TopicResolver

	// calendarGlanceFn builds the ambient upcoming-events glance for the dynamic
	// system-prompt block. nil disables ambient calendar awareness.
	calendarGlanceFn CalendarGlanceFunc

	// personaOverrideFn returns the operator-edited 업무 persona text (Settings
	// prompt corner), or "" when unedited. nil disables the override (the
	// default persona renders). Read per turn — byte-stable between rare edits,
	// so the Static cache holds; an edit changes the PersonaCacheKey and thus the
	// cache slot. The chat package stays free of the prompts/server import by
	// talking through this closure (wired in server/chat_pipeline.go).
	personaOverrideFn PersonaOverrideFunc

	// fileRecallFn runs a hybrid semantic search over the on-box file store for
	// the recall preflight, surfacing relevant uploaded files as recall evidence
	// (injected into the last user message tail, like the other recall sources).
	// nil disables the files recall source. Wired in server/chat_pipeline.go from
	// the shared file semantic index.
	fileRecallFn FileRecallFunc

	// chatport holds injected adapters that decouple chat from autoreply.
	chatport chatportAdapters
}

// PersonaOverrideFunc returns the operator-edited 업무 persona override text, or
// "" when there is no override. The concrete implementation lives in the server
// package and reads the prompt store; the chat package stays free of any
// infra/config import by talking through this function. nil disables the
// override entirely (DefaultPersona renders).
type PersonaOverrideFunc func() string

// abbreviateSession shortens channel prefixes in session keys for compact log output.
// e.g. "client:main:task:ts" → "cl:main:task:ts"
func abbreviateSession(key string) string {
	prefixes := [][2]string{
		{"client:", "cl:"},
	}
	for _, p := range prefixes {
		if len(key) > len(p[0]) && key[:len(p[0])] == p[0] {
			return p[1] + key[len(p[0]):]
		}
	}
	return key
}

// isSystemSession reports whether key is a system-internal session (e.g. "system:diary-heartbeat").
// System sessions must not write to the shared Aurora store because their messages
// (diary prompts, heartbeat responses) would contaminate the user's conversation context.
func isSystemSession(key string) bool {
	return strings.HasPrefix(key, "system:")
}

// isChatbotSessionKey reports whether key is a 챗봇-workspace session. The
// native client puts focused general chat under the "chat:" namespace and 업무
// work chat under "client:" (mirrors the client's isChatWorkspaceKey). The
// gateway uses this to route 챗봇 turns to RoleChatbot when an operator has
// assigned a separate chatbot model (see resolveModel).
func isChatbotSessionKey(key string) bool {
	return strings.HasPrefix(key, "chat:")
}

// isMainSession reports whether key is a top-level direct session (e.g. "client:main").
// Sub-sessions ("client:main:task:ts"), system ("system:*"), cron, hook, and
// bare keys (no colon, e.g. "dev-chat-xxx") return false.
func isMainSession(key string) bool {
	if isSystemSession(key) {
		return false
	}
	idx := strings.Index(key, ":")
	if idx < 0 {
		return false
	}
	return !strings.Contains(key[idx+1:], ":")
}

// runAgentAsync is the background goroutine that executes an agent run.
// It persists the user message, assembles context, calls the LLM agent loop,
// persists the result, and broadcasts completion events.
func runAgentAsync(ctx context.Context, params RunParams, deps runDeps) {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	var logArgs []any
	if !isMainSession(params.SessionKey) {
		logArgs = append(logArgs, "session", abbreviateSession(params.SessionKey))
	}
	if params.ClientRunID != "" {
		logArgs = append(logArgs, "runId", params.ClientRunID)
	}
	if len(logArgs) > 0 {
		logger = logger.With(logArgs...)
	}

	// Emit lifecycle start event for agent job tracker.
	if deps.jobTracker != nil {
		deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
			RunID: params.ClientRunID,
			Phase: "start",
			Ts:    time.Now().UnixMilli(),
		})
	}

	// Create streaming broadcaster for this run.
	var broadcaster *streaming.Broadcaster
	if deps.callbacks.broadcastRaw != nil {
		broadcaster = streaming.NewBroadcaster(deps.callbacks.broadcastRaw, params.SessionKey, params.ClientRunID)
		broadcaster.EmitStarted()
	}

	// Inject delivery context and reply function into ctx so tools
	// (especially the message tool) can send proactive messages.
	if params.Delivery != nil {
		ctx = WithDeliveryContext(ctx, params.Delivery)
	}
	if deps.callbacks.replyFunc != nil {
		ctx = WithReplyFunc(ctx, deps.callbacks.replyFunc)
	} else if deps.logger != nil {
		// Diagnostic for the self-contradicting "채널이 끊겼어요" cron
		// incident class: when this branch fires, the in-loop message tool
		// will trip its replyFn-nil guard and (without the new wording in
		// tools/message.go) the LLM has historically translated that into a
		// user-facing "channel down" report that itself gets delivered via
		// the cron proactive-relay path. Capture the sessionKey/delivery so
		// the next occurrence is debuggable from logs alone — wiring-order
		// audits of New() / registerLateMethods() did not reproduce it
		// statically, so we need runtime evidence to localise the regression.
		var deliveryChannel, deliveryTo string
		if params.Delivery != nil {
			deliveryChannel = params.Delivery.Channel
			deliveryTo = params.Delivery.To
		}
		deps.logger.Warn("run started without ReplyFunc in callbacks; in-loop message tool will fail with replyFn=nil",
			"sessionKey", params.SessionKey,
			"runID", params.ClientRunID,
			"deliveryChannel", deliveryChannel,
			"deliveryTo", deliveryTo,
			"hasDelivery", params.Delivery != nil)
	}
	// Scheduled/cron runs deliver their final text via the run-completion
	// layer, so an in-loop message-tool send failure is a benign no-op rather
	// than an outage the model should report. The message tool reads this flag.
	if params.AutoDeliveredOutput {
		ctx = WithAutoDelivery(ctx)
	}
	if deps.callbacks.mediaSendFn != nil {
		ctx = WithMediaSendFunc(ctx, deps.callbacks.mediaSendFn)
	}
	// Inject the channel-specific upload limit so send_file can enforce
	// the correct per-channel maximum without hard-coding channel names.
	if deps.channelUploadLimitFn != nil && params.Delivery != nil {
		if limit := deps.channelUploadLimitFn(params.Delivery.Channel); limit > 0 {
			ctx = WithMaxUploadBytes(ctx, limit)
		}
	}
	ctx = WithSessionKey(ctx, params.SessionKey)

	// Set up phase-aware typing indicator for native-client delivery.
	// The factory (injected via chatport boundary) creates a TypingSignaler with
	// a 5s keepalive cadence for the native typing indicator.
	var typingSignaler chatport.TypingSignaler
	if deps.chatport.NewTypingSignaler != nil && deps.callbacks.typingFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		typingSignaler = deps.chatport.NewTypingSignaler(func() { _ = deps.callbacks.typingFn(ctx, delivery) })
		typingSignaler.SignalRunStart()
	}

	// Status reactions (phase-aware emoji on the user's message) were a
	// Telegram-only affordance, retired with the bot. statusCtrl stays nil and
	// the guarded calls downstream are inert; the native client surfaces run
	// phase via structured WebSocket/SSE events instead of message reactions.
	var statusCtrl statusReactor

	// Create agent detail logger for this run.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)

	// Run the agent and capture result.
	chatResult, err := executeAgentRun(ctx, params, deps, broadcaster, typingSignaler, statusCtrl, logger, runLog)

	// Stop typing indicator before delivering the reply.
	if typingSignaler != nil {
		typingSignaler.Stop()
	}

	// Persist interrupted context: when the run was aborted while tools were
	// executing, save a context note to the transcript so the next run knows
	// what the assistant was doing. Without this, the next run has no memory
	// of the interrupted work and starts from scratch.
	if chatResult != nil && len(chatResult.InterruptedToolNames) > 0 && deps.transcript != nil {
		persistInterruptedContext(deps, params.SessionKey, chatResult.AgentResult, logger)
	}

	// Handle completion.
	now := time.Now().UnixMilli()

	// A run cancelled by a quick-fire merge can land on EITHER branch:
	//   - error path: LLM call returned context.Canceled / DeadlineExceeded
	//   - success path: agent loop saw ctx.Done() between turns and
	//     returned cleanly with stopReason="aborted" (no error)
	// In both cases the user's intent is "supersede with the next run",
	// so we clear the emoji instead of finishing with 👍 (Done) or 😱
	// (Error). The new run sets its own emoji on the new user message.
	mergedCancel := errors.Is(context.Cause(ctx), ErrMergedIntoNewRun)

	if err != nil {
		if statusCtrl != nil {
			if mergedCancel {
				statusCtrl.SetClear()
			} else {
				statusCtrl.SetError()
			}
			statusCtrl.CloseAfterDrain()
		}
		handleRunError(ctx, params, deps, broadcaster, logger, err, now)

		// Drain pending queue even on error: if the user sent a message while
		// this run was active, it must be processed regardless of whether the
		// run succeeded or failed. Without this, queued messages are silently
		// lost when the LLM stalls or the run errors out.
		if deps.drainPendingFn != nil && deps.startRunFn != nil {
			if pending := deps.drainPendingFn(params.SessionKey); pending != nil {
				logger.Info("processing queued message after run error",
					"sessionKey", params.SessionKey)
				deps.startRunFn(*pending)
			}
		}
		return
	}

	if statusCtrl != nil {
		if mergedCancel {
			statusCtrl.SetClear()
		} else {
			statusCtrl.SetDone()
		}
		statusCtrl.CloseAfterDrain()
	}

	// Skip handleRunSuccess on a merge cancel: there's no real assistant
	// response to deliver (the new run will produce one), and dispatching
	// an empty/aborted reply would surface "agent produced empty response"
	// noise to the channel layer.
	if mergedCancel {
		return
	}
	handleRunSuccess(ctx, params, deps, broadcaster, logger, chatResult.AgentResult, now)

	// Process pending message: if the user sent a message while this run was
	// active, it was queued. Now that the run is complete, drain and process it.
	if deps.drainPendingFn != nil && deps.startRunFn != nil {
		if pending := deps.drainPendingFn(params.SessionKey); pending != nil {
			logger.Info("processing queued message after run completion",
				"sessionKey", params.SessionKey)
			deps.startRunFn(*pending)
			return
		}
	}
}
