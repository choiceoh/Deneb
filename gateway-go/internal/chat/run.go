package chat

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
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
	Model        string // role name ("main", "lightweight", "fallback", "image"); raw model ID only via /model override
	System       string // system prompt override
	ClientRunID  string
	Delivery     *DeliveryContext
	WorkspaceDir string // per-channel workspace override (empty = use global default)
}

// Agent run defaults.
const (
	defaultMaxTokens     = 8192
	defaultMaxTurns      = 25
	defaultAgentTimeout  = 10 * time.Minute
	maxCompactionRetries = 2
)

// runDeps holds the dependencies the async run needs from the Handler.
// Optional fields (may be nil): transcript, tools, authManager,
// broadcast, broadcastRaw, jobTracker. Required: sessions, logger.
type runDeps struct {
	sessions         *session.Manager           // required
	llmClient        *llm.Client                // optional; resolved from authManager if nil
	transcript       TranscriptStore            // optional; history unavailable without it
	tools            *ToolRegistry              // optional; no tool use if nil
	authManager      *provider.AuthManager      // optional; uses pre-configured client if nil
	broadcast        BroadcastFunc              // optional
	broadcastRaw     streaming.BroadcastRawFunc // optional
	jobTracker       *agent.JobTracker          // optional
	replyFunc        ReplyFunc                  // optional; delivers response to originating channel
	mediaSendFn      MediaSendFunc              // optional; delivers files to originating channel
	typingFn         TypingFunc                 // optional; sends typing indicator during run
	reactionFn       ReactionFunc               // optional; sets emoji reaction for status phases
	removeReactionFn ReactionFunc               // optional; removes emoji reaction (Discord additive)
	// channelUploadLimitFn returns the max file upload size for a channel ID.
	// Returns 0 if no limit is registered (tool applies its own default).
	channelUploadLimitFn func(channelID string) int64 // optional
	toolProgressFn       ToolProgressFunc             // optional; reports tool events to Discord
	providerConfigs      map[string]ProviderConfig    // optional; config-based provider credentials
	logger               *slog.Logger                 // required (defaults to slog.Default)

	auroraStore    *aurora.Store             // optional; enables Aurora compaction
	vegaBackend    vega.Backend              // optional; enables knowledge prefetch
	memoryStore    *memory.Store             // optional; structured memory (Honcho-style)
	memoryEmbedder *memory.Embedder          // optional; fact embedding
	dreamTurnFn    func(ctx context.Context) // optional; increments dream turn via autonomous
	agentLog       *agentlog.Writer          // optional; enables agent detail logging
	registry       *modelrole.Registry       // centralized model role registry
	contextCfg     ContextConfig
	compactionCfg  CompactionConfig
	defaultModel   string
	defaultSystem  string
	maxTokens      int
	// shutdownCtx is the server lifecycle context; used to bound background
	// goroutines (e.g., auto-memory extraction) so they stop on server shutdown.
	shutdownCtx context.Context
}

// abbreviateSession shortens channel prefixes in session keys for compact log output.
// e.g. "telegram:7074071666" → "te:7074071666", "discord:123" → "dc:123"
func abbreviateSession(key string) string {
	prefixes := [][2]string{
		{"telegram:", "te:"},
		{"discord:", "dc:"},
	}
	for _, p := range prefixes {
		if len(key) > len(p[0]) && key[:len(p[0])] == p[0] {
			return p[1] + key[len(p[0]):]
		}
	}
	return key
}

// isMainSession reports whether key is a top-level direct session (e.g. "telegram:123").
// Sub-sessions ("telegram:123:task:ts"), cron, and hook sessions return false.
func isMainSession(key string) bool {
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
	if deps.broadcastRaw != nil {
		broadcaster = streaming.NewBroadcaster(deps.broadcastRaw, params.SessionKey, params.ClientRunID)
		broadcaster.EmitStarted()
	}

	// Inject delivery context and reply function into ctx so tools
	// (especially the message tool) can send proactive messages.
	if params.Delivery != nil {
		ctx = WithDeliveryContext(ctx, params.Delivery)
	}
	if deps.replyFunc != nil {
		ctx = WithReplyFunc(ctx, deps.replyFunc)
	}
	if deps.mediaSendFn != nil {
		ctx = WithMediaSendFunc(ctx, deps.mediaSendFn)
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
	// Uses TypingController (5s keepalive matching Telegram's sendChatAction TTL)
	// with FullTypingSignaler for phase-aware signals (text, thinking, tool use).
	var typingSignaler *typing.FullTypingSignaler
	if deps.typingFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		typingCtrl := typing.NewTypingController(typing.TypingControllerConfig{
			OnStart:    func() { _ = deps.typingFn(ctx, delivery) },
			IntervalMs: 5000, // Telegram typing expires after 5s
		})
		typingSignaler = typing.NewFullTypingSignaler(typingCtrl, typing.TypingModeInstant, false)
		typingSignaler.SignalRunStart()
	}

	// Set up status reaction controller for phase-aware emoji on the user's message.
	// Shows: 👀 queued → 🤔 thinking → 🔥 tool → ⚡ web → 👍 done.
	var statusCtrl *channel.StatusReactionController
	if deps.reactionFn != nil && params.Delivery != nil && params.Delivery.MessageID != "" {
		delivery := params.Delivery
		phaseEmojis := channel.StatusReactionEmojis{
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
		adapter := channel.StatusReactionAdapter{
			SetReaction: func(emoji string) error {
				rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				return deps.reactionFn(rctx, delivery, emoji)
			},
		}
		// Discord uses additive reactions: need RemoveReaction to clear previous phase.
		if deliveryChannel(params.Delivery) == "discord" && deps.removeReactionFn != nil {
			adapter.RemoveReaction = func(emoji string) error {
				rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				return deps.removeReactionFn(rctx, delivery, emoji)
			}
		}
		statusCtrl = channel.NewStatusReactionController(channel.StatusReactionControllerParams{
			Enabled: true,
			Adapter: adapter,
			Emojis:  &phaseEmojis,
			OnError: func(err error) {
				logger.Warn("status reaction failed", "error", err)
			},
		})
		statusCtrl.SetQueued()
	}

	// Create agent detail logger for this run.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)

	// Run the agent and capture result.
	result, err := executeAgentRun(ctx, params, deps, broadcaster, typingSignaler, statusCtrl, logger, runLog)

	// Stop typing indicator before delivering the reply.
	if typingSignaler != nil {
		typingSignaler.Stop()
	}

	// Handle completion.
	now := time.Now().UnixMilli()
	if err != nil {
		if statusCtrl != nil {
			statusCtrl.SetError()
			statusCtrl.CloseAfterDrain()
		}
		handleRunError(ctx, params, deps, broadcaster, logger, err, now, runLog)
		return
	}

	if statusCtrl != nil {
		statusCtrl.SetDone()
		statusCtrl.CloseAfterDrain()
	}
	handleRunSuccess(ctx, params, deps, broadcaster, logger, result, now, runLog)
}
