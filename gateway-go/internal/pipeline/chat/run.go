package chat

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
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

	// PrebuiltMessages, when set, replaces the normal transcript-based context
	// assembly. Used by the OpenAI-compatible HTTP API to pass through the full
	// conversation history from the client.
	PrebuiltMessages []llm.Message
}

// Agent run defaults.
const (
	defaultMaxTokens    = 8192
	defaultMaxTurns     = 25
	defaultAgentTimeout = 60 * time.Minute
)

// chatportAdapters holds injected implementations that decouple chat from autoreply.
// When nil, the corresponding functionality is simply skipped.
type chatportAdapters struct {
	NewTypingSignaler    func(onStart func()) chatport.TypingSignaler // optional; creates phase-aware typing signaler
	SanitizeDraft        chatport.DraftSanitizerFunc                  // optional; cleans streaming draft text
	ParseReplyDirectives chatport.ParseReplyDirectivesFunc            // optional; parses reply directives
	IsTransientError     chatport.IsTransientErrorFunc                // optional; checks transient HTTP errors
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

	wikiStore            *wiki.Store               // optional; wiki knowledge base
	dreamTurnFn          func(ctx context.Context) // optional; increments dream turn via autonomous
	agentLog             *agentlog.Writer          // optional; enables agent detail logging
	registry             *modelrole.Registry       // centralized model role registry
	contextCfg           ContextConfig
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int
	// internalHookRegistry fires programmatic internal hooks.
	internalHookRegistry *hooks.InternalRegistry
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

	// callbacks is an atomic snapshot of channel callbacks taken at run start.
	// Contains reply, media, typing, reaction, draft, emit, shutdown, and model fields.
	callbacks CallbackSnapshot

	// chatport holds injected adapters that decouple chat from autoreply.
	chatport chatportAdapters
}

// abbreviateSession shortens channel prefixes in session keys for compact log output.
// e.g. "telegram:7074071666" → "te:7074071666"
func abbreviateSession(key string) string {
	prefixes := [][2]string{
		{"telegram:", "te:"},
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

// isMainSession reports whether key is a top-level direct session (e.g. "telegram:123").
// Sub-sessions ("telegram:123:task:ts"), system ("system:*"), cron, hook, and
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

	// Set up phase-aware typing indicator for channel delivery (e.g., Telegram).
	// The factory (injected via chatport boundary) creates a TypingSignaler with
	// a 5s keepalive matching Telegram's sendChatAction TTL.
	var typingSignaler chatport.TypingSignaler
	if deps.chatport.NewTypingSignaler != nil && deps.callbacks.typingFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		typingSignaler = deps.chatport.NewTypingSignaler(func() { _ = deps.callbacks.typingFn(ctx, delivery) })
		typingSignaler.SignalRunStart()
	}

	// Set up status reaction controller for phase-aware emoji on the user's message.
	// Shows: 👀 queued → 🤔 thinking → 🔥 tool → ⚡ web → 👍 done.
	var statusCtrl *telegram.StatusReactionController
	if deps.callbacks.reactionFn != nil && params.Delivery != nil && params.Delivery.MessageID != "" {
		delivery := params.Delivery
		phaseEmojis := telegram.StatusReactionEmojis{
			Queued:     "👀",
			Thinking:   "🤔",
			Tool:       "🔥",
			Coding:     "🔥",
			Web:        "⚡",
			Done:       "👍",
			Error:      "😱",
			StallSoft:  "🥱",
			StallHard:  "😨",
			Compacting: "🤔",
		}
		statusCtrl = telegram.NewStatusReactionController(telegram.StatusReactionControllerParams{
			Enabled: true,
			SetReaction: func(emoji string) error {
				rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				return deps.callbacks.reactionFn(rctx, delivery, emoji)
			},
			Emojis: &phaseEmojis,
			OnError: func(err error) {
				logger.Warn("status reaction failed", "error", err)
			},
		})
		statusCtrl.SetQueued()
	}

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
	if err != nil {
		if statusCtrl != nil {
			statusCtrl.SetError()
			statusCtrl.CloseAfterDrain()
		}
		handleRunError(ctx, params, deps, broadcaster, logger, err, now, runLog)

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
		statusCtrl.SetDone()
		statusCtrl.CloseAfterDrain()
	}
	handleRunSuccess(ctx, params, deps, broadcaster, logger, chatResult.AgentResult, now, runLog)

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
