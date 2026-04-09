package chat

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Handler manages chat RPC methods.
type Handler struct {
	sessions     *session.Manager
	broadcast    BroadcastFunc
	broadcastRaw streaming.BroadcastRawFunc
	logger       *slog.Logger

	// Native agent execution deps.
	llmClient       *llm.Client
	transcript      TranscriptStore
	tools           *ToolRegistry
	authManager     *provider.AuthManager
	jobTracker      *agent.JobTracker
	providerConfigs map[string]ProviderConfig
	wikiStore       *wiki.Store                       // optional; wiki knowledge base
	dreamTurnFn     func(ctx context.Context)         // optional; increments dream turn via autonomous
	agentLog        *agentlog.Writer                  // optional; agent detail logging
	registry        *modelrole.Registry               // centralized model role registry
	providerRuntime *provider.ProviderRuntimeResolver // optional; runtime auth, missing-auth messages

	// Agent run configuration.
	contextCfg           ContextConfig
	defaultModel         string
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int

	// callbackMu guards all late-bind callback fields below. These are set
	// during server initialization (before HTTP serving starts) and read
	// during request handling. The mutex ensures safe access even if the
	// init/serve boundary is refactored in the future.
	callbackMu sync.RWMutex

	replyFunc        ReplyFunc       // optional: delivers response to originating channel
	mediaSendFn      MediaSendFunc   // optional: delivers files to originating channel
	typingFn         TypingFunc      // optional: sends typing indicator during agent run
	reactionFn       ReactionFunc    // optional: sets emoji reaction on triggering message
	removeReactionFn ReactionFunc    // optional: removes emoji reaction
	draftEditFn      DraftEditFunc   // optional: sends/edits streaming draft messages
	draftDeleteFn    DraftDeleteFunc // optional: deletes streaming draft messages
	// emitAgentFn sends agent lifecycle events to gateway event subscriptions.
	emitAgentFn func(kind, sessionKey, runID string, payload map[string]any)
	// emitTranscriptFn sends transcript updates to gateway event subscriptions.
	emitTranscriptFn func(sessionKey string, message any, messageID string)

	// uploadLimitsMu guards uploadLimits.
	uploadLimitsMu sync.RWMutex
	// uploadLimits maps channelID → max file upload size in bytes.
	// Populated by SetChannelUploadLimit during channel wiring.
	uploadLimits map[string]int64

	// shutdownCtx is the server lifecycle context. Set via SetShutdownCtx so
	// background goroutines (auto-memory extraction) stop on server shutdown.
	// Protected by callbackMu.
	shutdownCtx context.Context

	abortMu  sync.Mutex
	abortMap map[string]*AbortEntry // clientRunId -> entry
	done     chan struct{}          // signals abortGCLoop to stop

	// pendingMu guards pendingMsgs.
	pendingMu   sync.Mutex
	pendingMsgs map[string]*pendingRunQueue // sessionKey -> queued messages

	// subagentNotifyMu guards subagentNotifyChs and subagentNotifyQueues.
	subagentNotifyMu sync.Mutex
	// subagentNotifyChs maps parent sessionKey → buffered channel for receiving
	// child completion notifications. Created lazily per parent session, consumed
	// by DeferredSystemText during agent runs.
	subagentNotifyChs map[string]chan string
	// subagentNotifyQueues maps parent sessionKey → debounced notification queue.
	// Batches concurrent child completions (1s debounce) before flushing.
	subagentNotifyQueues map[string]*notifyQueue

	// runStateMachine tracks active agent runs for status broadcasting.
	// Protected by callbackMu.
	runStateMachine *telegram.RunStateMachine

	// internalHookRegistry fires programmatic internal hooks on message/tool events.
	internalHookRegistry *hooks.InternalRegistry

	// statusDepsFunc returns server-level status data for /status command.
	// Injected by the server after handler creation.
	statusDepsFunc StatusDepsFunc

	// maxHistoryBytes caps the total JSON bytes returned by chat.history.
	maxHistoryBytes int
	// maxHistoryCount caps the number of messages returned.
	maxHistoryCount int
	// maxMessageBytes caps a single message body before truncation.
	maxMessageBytes int
}

// HandlerConfig configures the chat handler.
type HandlerConfig struct {
	MaxHistoryBytes int
	MaxHistoryCount int
	MaxMessageBytes int

	// Native agent execution config.
	LLMClient            *llm.Client
	Transcript           TranscriptStore
	Tools                *ToolRegistry
	AuthManager          *provider.AuthManager
	JobTracker           *agent.JobTracker
	ProviderConfigs      map[string]ProviderConfig // provider ID → config
	WikiStore            *wiki.Store               // optional; wiki knowledge base
	DreamTurnFn          func(ctx context.Context) // optional; increments dream turn via autonomous
	AgentLog             *agentlog.Writer          // optional; agent detail logging
	Registry             *modelrole.Registry       // centralized model role registry
	LocalAIHub           *localai.Hub              // centralized local AI request hub
	ContextCfg           ContextConfig
	DefaultModel         string
	SubagentDefaultModel string // separate default model for sub-agents (from agents.defaults.subagents.model)
	DefaultSystem        string
	MaxTokens            int

	// Fields below were previously Set*() after construction. They are all
	// available at handler creation time and passed here to reduce late-binding.
	ProviderRuntime      *provider.ProviderRuntimeResolver // optional; runtime auth
	InternalHookRegistry *hooks.InternalRegistry           // optional; programmatic internal hooks
	BroadcastRaw         streaming.BroadcastRawFunc        // optional; raw event relay
	EmitAgentFn          func(kind, sessionKey, runID string, payload map[string]any)
	EmitTranscriptFn     func(sessionKey string, message any, messageID string)
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		MaxHistoryBytes: 2 * 1024 * 1024, // 2 MB
		MaxHistoryCount: 200,
		MaxMessageBytes: 128 * 1024, // 128 KB
		ContextCfg:      DefaultContextConfig(),
		MaxTokens:       defaultMaxTokens,
	}
}

// NewHandler creates a new chat handler.
func NewHandler(sessions *session.Manager, broadcast BroadcastFunc, logger *slog.Logger, cfg HandlerConfig) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxHistoryBytes == 0 {
		defaults := DefaultHandlerConfig()
		cfg.MaxHistoryBytes = defaults.MaxHistoryBytes
		cfg.MaxHistoryCount = defaults.MaxHistoryCount
		cfg.MaxMessageBytes = defaults.MaxMessageBytes
	}
	h := &Handler{
		sessions:             sessions,
		broadcast:            broadcast,
		logger:               logger,
		llmClient:            cfg.LLMClient,
		transcript:           cfg.Transcript,
		tools:                cfg.Tools,
		authManager:          cfg.AuthManager,
		jobTracker:           cfg.JobTracker,
		providerConfigs:      cfg.ProviderConfigs,
		wikiStore:            cfg.WikiStore,
		dreamTurnFn:          cfg.DreamTurnFn,
		agentLog:             cfg.AgentLog,
		registry:             cfg.Registry,
		contextCfg:           cfg.ContextCfg,
		defaultModel:         cfg.DefaultModel,
		subagentDefaultModel: cfg.SubagentDefaultModel,
		defaultSystem:        cfg.DefaultSystem,
		maxTokens:            cfg.MaxTokens,
		providerRuntime:      cfg.ProviderRuntime,
		internalHookRegistry: cfg.InternalHookRegistry,
		broadcastRaw:         cfg.BroadcastRaw,
		emitAgentFn:          cfg.EmitAgentFn,
		emitTranscriptFn:     cfg.EmitTranscriptFn,
		abortMap:             make(map[string]*AbortEntry),
		pendingMsgs:          make(map[string]*pendingRunQueue),
		subagentNotifyChs:    make(map[string]chan string),
		subagentNotifyQueues: make(map[string]*notifyQueue),
		uploadLimits:         make(map[string]int64),
		done:                 make(chan struct{}),
		maxHistoryBytes:      cfg.MaxHistoryBytes,
		maxHistoryCount:      cfg.MaxHistoryCount,
		maxMessageBytes:      cfg.MaxMessageBytes,
	}
	// Set the package-level model role registry for local AI hooks.
	if h.registry != nil {
		pilot.SetModelRoleRegistry(h.registry)
	}
	// Wire centralized local AI hub for token budget management and health checks.
	if cfg.LocalAIHub != nil {
		pilot.SetLocalAIHub(cfg.LocalAIHub)
	}
	go h.abortGCLoop()
	h.startSubagentNotifier()
	return h
}

// SetBroadcastRaw sets the raw broadcast function for streaming event relay.
func (h *Handler) SetBroadcastRaw(fn streaming.BroadcastRawFunc) {
	h.callbackMu.Lock()
	h.broadcastRaw = fn
	h.callbackMu.Unlock()
}

// SetReplyFunc sets the function that delivers assistant responses back to the
// originating channel (e.g., Telegram). Called after each successful agent run
// when a DeliveryContext is present.
func (h *Handler) SetReplyFunc(fn ReplyFunc) {
	h.callbackMu.Lock()
	h.replyFunc = fn
	h.callbackMu.Unlock()
}

// SetMediaSendFunc sets the function that delivers files back to the
// originating channel (e.g., Telegram). Used by the send_file tool.
func (h *Handler) SetMediaSendFunc(fn MediaSendFunc) {
	h.callbackMu.Lock()
	h.mediaSendFn = fn
	h.callbackMu.Unlock()
}

// SetTypingFunc sets the function that sends typing indicators to the
// originating channel (e.g., Telegram "typing..." status) during agent runs.
func (h *Handler) SetTypingFunc(fn TypingFunc) {
	h.callbackMu.Lock()
	h.typingFn = fn
	h.callbackMu.Unlock()
}

// SetReactionFunc sets the function that manages emoji reactions on the
// triggering message to indicate agent status phases (thinking, tool use, done).
func (h *Handler) SetReactionFunc(fn ReactionFunc) {
	h.callbackMu.Lock()
	h.reactionFn = fn
	h.callbackMu.Unlock()
}

// SetRemoveReactionFunc sets the function that removes an emoji reaction
// from the triggering message when channel adapters require explicit cleanup.
func (h *Handler) SetRemoveReactionFunc(fn ReactionFunc) {
	h.callbackMu.Lock()
	h.removeReactionFn = fn
	h.callbackMu.Unlock()
}

// SetDraftEditFunc sets the function that sends/edits streaming draft messages
// on the originating channel for real-time LLM output display.
func (h *Handler) SetDraftEditFunc(fn DraftEditFunc) {
	h.callbackMu.Lock()
	h.draftEditFn = fn
	h.callbackMu.Unlock()
}

// SetDraftDeleteFunc sets the function that deletes streaming draft messages
// on the originating channel, used to clean up partial drafts before the final reply.
func (h *Handler) SetDraftDeleteFunc(fn DraftDeleteFunc) {
	h.callbackMu.Lock()
	h.draftDeleteFn = fn
	h.callbackMu.Unlock()
}

// SetChannelUploadLimit registers the maximum file upload size for a channel.
// Called once per channel during server wiring (e.g., wireTelegramChatHandler).
func (h *Handler) SetChannelUploadLimit(channelID string, maxBytes int64) {
	h.uploadLimitsMu.Lock()
	h.uploadLimits[channelID] = maxBytes
	h.uploadLimitsMu.Unlock()
}

// ChannelUploadLimit returns the registered upload limit for channelID,
// or 0 if no limit has been registered for that channel.
func (h *Handler) ChannelUploadLimit(channelID string) int64 {
	h.uploadLimitsMu.RLock()
	n := h.uploadLimits[channelID]
	h.uploadLimitsMu.RUnlock()
	return n
}

// RemoveReactionFunc returns the current remove reaction function (for chaining).
func (h *Handler) RemoveReactionFunc() ReactionFunc {
	h.callbackMu.RLock()
	fn := h.removeReactionFn
	h.callbackMu.RUnlock()
	return fn
}

// ReplyFunc returns the current reply function (for chaining).
func (h *Handler) ReplyFunc() ReplyFunc {
	h.callbackMu.RLock()
	fn := h.replyFunc
	h.callbackMu.RUnlock()
	return fn
}

// MediaSendFunc returns the current media send function (for chaining).
func (h *Handler) MediaSendFunc() MediaSendFunc {
	h.callbackMu.RLock()
	fn := h.mediaSendFn
	h.callbackMu.RUnlock()
	return fn
}

// TypingFunc returns the current typing function (for chaining).
func (h *Handler) TypingFunc() TypingFunc {
	h.callbackMu.RLock()
	fn := h.typingFn
	h.callbackMu.RUnlock()
	return fn
}

// ReactionFunc returns the current reaction function (for chaining).
func (h *Handler) ReactionFunc() ReactionFunc {
	h.callbackMu.RLock()
	fn := h.reactionFn
	h.callbackMu.RUnlock()
	return fn
}

// SetDefaultModel sets the default model ID for subsequent agent runs.
func (h *Handler) SetDefaultModel(model string) {
	h.callbackMu.Lock()
	h.defaultModel = model
	h.callbackMu.Unlock()
}

// SetShutdownCtx sets the server lifecycle context so background goroutines
// (e.g., auto-memory extraction) are cancelled when the server shuts down.
func (h *Handler) SetShutdownCtx(ctx context.Context) {
	h.callbackMu.Lock()
	h.shutdownCtx = ctx
	h.callbackMu.Unlock()
}

// SetRunStateMachine sets the state machine that tracks active agent runs.
func (h *Handler) SetRunStateMachine(sm *telegram.RunStateMachine) {
	h.callbackMu.Lock()
	h.runStateMachine = sm
	h.callbackMu.Unlock()
}

// StatusDepsFunc returns server-level status data for the /status command.
// Called lazily so values are always fresh.
type StatusDepsFunc func(sessionKey string) StatusDeps

// StatusDeps holds server-level data for the /status command.
type StatusDeps struct {
	Version           string
	StartedAt         time.Time
	SessionCount      int
	ActiveRuns        int
	LastFailureReason string
}

// SetStatusDepsFunc sets the callback that provides server-level status data.
func (h *Handler) SetStatusDepsFunc(fn StatusDepsFunc) {
	h.callbackMu.Lock()
	h.statusDepsFunc = fn
	h.callbackMu.Unlock()
}

// DefaultModel returns the configured default LLM model name.
func (h *Handler) DefaultModel() string {
	h.callbackMu.RLock()
	m := h.defaultModel
	h.callbackMu.RUnlock()
	return m
}

// ModelRegistry returns the centralized model role registry.
func (h *Handler) ModelRegistry() *modelrole.Registry {
	return h.registry
}

// Close stops background goroutines and cancels all active abort entries.
func (h *Handler) Close() {
	// Signal abortGCLoop to exit.
	select {
	case <-h.done:
	default:
		close(h.done)
	}

	h.abortMu.Lock()
	for _, entry := range h.abortMap {
		entry.CancelFn()
	}
	h.abortMap = make(map[string]*AbortEntry)
	h.abortMu.Unlock()

	h.pendingMu.Lock()
	h.pendingMsgs = make(map[string]*pendingRunQueue)
	h.pendingMu.Unlock()

	h.subagentNotifyMu.Lock()
	h.subagentNotifyChs = make(map[string]chan string)
	h.subagentNotifyQueues = make(map[string]*notifyQueue)
	h.subagentNotifyMu.Unlock()
}
