package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// cachedWorkspaceDir caches the resolved workspace directory at startup
// to avoid disk I/O (config.LoadConfigFromDefaultPath) on every chat message.
// Single-user deployment: config doesn't change at runtime.
var (
	cachedWorkspaceDir     string
	cachedWorkspaceDirOnce sync.Once
)

// RunParams holds all parameters for an asynchronous agent run.
type RunParams = runstate.Params

// Agent run defaults.
const (
	defaultMaxTokens    = 32768
	defaultMaxTurns     = 50
	defaultAgentTimeout = 60 * time.Minute
)

// RunLimits is a trusted, handler-scoped override for agent execution budgets.
// Zero values retain the normal mode-aware defaults.
type RunLimits struct {
	MaxTurns int
	Timeout  time.Duration
}

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

	// memory groups the memory/knowledge backends. See MemoryDeps.
	memory             MemoryDeps
	dreamTurnFn        func(ctx context.Context) // optional; increments dream turn via autonomous
	preferenceSignalFn func()                    // optional; notes a 선호-tagged diary capsule for accelerated dreaming
	// deliverablePublisher files the turn's final response as a doc_analysis
	// work-feed card — the server-side auto safety net for the deliverable → 작업
	// 피드 contract. Optional; nil disables (older wiring/tests).
	deliverablePublisher func(text string) (bool, error)
	// translateThinking renders the turn's extended-thinking text into Korean
	// for the 🧠 blockquote. Optional; nil leaves thinking in whatever language
	// the model produced. Display-only — the streamed `reasoningFull` payload
	// is untouched.
	translateThinking    func(ctx context.Context, text string) (string, bool)
	agentLog             *agentlog.Writer    // optional; enables agent detail logging
	registry             *modelrole.Registry // centralized model role registry
	contextCfg           ContextConfig
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int
	runLimits            RunLimits
	samplingSeed         *int64
	disableTier1Wiki     bool
	semanticNow          func() time.Time
	semanticTimezone     string
	workspaceDir         string // real agent workspace (MEMORY.md lives here)
	promptWorkspaceDir   string
	briefcaseMode        bool
	strictErrors         *strictRunErrorSink
	auditSystemPrompt    func(sessionKey string, prompt []byte)
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

	// skills groups the Propus/genesis skill-loop hooks. See SkillDeps.
	skills SkillDeps

	// callbacks is an atomic snapshot of channel callbacks taken at run start.
	// Contains reply, media, typing, reaction, draft, emit, shutdown, and model fields.
	callbacks CallbackSnapshot

	// ambient groups the ambient system-prompt context providers. The chat
	// package stays free of the prompts/server import by talking through
	// these closures (wired in server/chat_pipeline.go). See AmbientDeps.
	ambient AmbientDeps

	// chatport holds injected adapters that decouple chat from autoreply.
	chatport chatportAdapters

	// normalizeCardReply validates and repairs deneb-ui fences before final
	// persistence/delivery; reportCardHealth observes the normalized result.
	normalizeCardReply func(text, sessionKey string, logger *slog.Logger) string
	reportCardHealth   func(text, sessionKey string, logger *slog.Logger)
}

type strictRunErrorSink struct {
	mu  sync.Mutex
	err error
}

func (s *strictRunErrorSink) Record(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	s.err = errors.Join(s.err, err)
	s.mu.Unlock()
}

func (s *strictRunErrorSink) Err() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (d runDeps) now() time.Time {
	if d.semanticNow != nil {
		return d.semanticNow()
	}
	return dentime.Now()
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
	return session.IsSystemSession(key)
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
	logger := runAgentLoggerFor(params, deps)

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

	ctx = withRunContextValues(ctx, params, deps)
	if broadcaster != nil {
		// Refine the live "thinking" chip into a fast-model Korean progress line
		// (Option A). Uses the enriched run ctx so summaries stop when the run does.
		broadcaster.SetThinkingSummarizer(newThinkingSummarizer(ctx))
	}
	typingSignaler := startRunTypingSignaler(ctx, params, deps)

	// Create agent detail logger for this run.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)

	// Run the agent and capture result.
	chatResult, err := executeAgentRun(ctx, params, deps, broadcaster, typingSignaler, logger, runLog)

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
	// so the run is quietly superseded; the new run produces the reply.
	mergedCancel := errors.Is(context.Cause(ctx), ErrMergedIntoNewRun)

	if err != nil {
		handleRunError(ctx, params, deps, broadcaster, logger, err, now)

		// Drain pending queue even on error: if the user sent a message while
		// this run was active, it must be processed regardless of whether the
		// run succeeded or failed. Without this, queued messages are silently
		// lost when the LLM stalls or the run errors out.
		drainPendingAfterRun(params, deps, logger, "processing queued message after run error")
		return
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
	drainPendingAfterRun(params, deps, logger, "processing queued message after run completion")
}

// runAgentLoggerFor derives the per-run logger (session/runId annotations for
// non-main sessions) from the deps logger.
func runAgentLoggerFor(params RunParams, deps runDeps) *slog.Logger {
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
	return logger
}

// withRunContextValues injects the per-run context values tools read: delivery
// context, reply/media send functions, auto-delivery flag, channel upload
// limit, and session key.
func withRunContextValues(ctx context.Context, params RunParams, deps runDeps) context.Context {
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
	return WithSessionKey(ctx, params.SessionKey)
}

// startRunTypingSignaler sets up the phase-aware typing indicator for
// native-client delivery. The factory (injected via chatport boundary) creates
// a TypingSignaler with a 5s keepalive cadence for the native typing
// indicator. Returns nil when typing signalling is not wired for this run.
func startRunTypingSignaler(ctx context.Context, params RunParams, deps runDeps) chatport.TypingSignaler {
	if deps.chatport.NewTypingSignaler == nil || deps.callbacks.typingFn == nil || params.Delivery == nil {
		return nil
	}
	delivery := params.Delivery
	typingSignaler := deps.chatport.NewTypingSignaler(func() { _ = deps.callbacks.typingFn(ctx, delivery) })
	typingSignaler.SignalRunStart()
	return typingSignaler
}

// drainPendingAfterRun processes one queued message after a run finishes (on
// both the error and success paths), so messages sent mid-run are never
// silently lost.
func drainPendingAfterRun(params RunParams, deps runDeps, logger *slog.Logger, logMsg string) {
	if deps.drainPendingFn == nil || deps.startRunFn == nil {
		return
	}
	if pending := deps.drainPendingFn(params.SessionKey); pending != nil {
		logger.Info(logMsg, "sessionKey", params.SessionKey)
		deps.startRunFn(*pending)
	}
}
