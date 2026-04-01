package chat

import (
	"context"
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
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
	auroraStore     *aurora.Store                     // Aurora hierarchical compaction store
	vegaBackend     vega.Backend                      // optional; knowledge prefetch
	memoryStore     *memory.Store                     // optional; structured memory (Honcho-style)
	sessionMemory   *SessionMemoryStore               // optional; structured session working state
	memoryEmbedder  *memory.Embedder                  // optional; fact embedding
	unifiedStore    *unified.Store                    // optional; unified memory (search + tier-1)
	dreamTurnFn     func(ctx context.Context)         // optional; increments dream turn via autonomous
	agentLog        *agentlog.Writer                  // optional; agent detail logging
	registry        *modelrole.Registry               // centralized model role registry
	providerRuntime *provider.ProviderRuntimeResolver // optional; runtime auth, missing-auth messages

	// Agent run configuration.
	contextCfg    ContextConfig
	compactionCfg CompactionConfig
	defaultModel  string
	defaultSystem string
	maxTokens     int

	replyFunc        ReplyFunc        // optional: delivers response to originating channel
	mediaSendFn      MediaSendFunc    // optional: delivers files to originating channel
	typingFn         TypingFunc       // optional: sends typing indicator during agent run
	reactionFn       ReactionFunc     // optional: sets emoji reaction on triggering message
	removeReactionFn ReactionFunc     // optional: removes emoji reaction
	toolProgressFn   ToolProgressFunc // optional: reports tool execution events
	draftEditFn      DraftEditFunc    // optional: sends/edits streaming draft messages
	draftDeleteFn    DraftDeleteFunc  // optional: deletes streaming draft messages
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
	shutdownCtx context.Context

	abortMu  sync.Mutex
	abortMap map[string]*AbortEntry // clientRunId -> entry
	done     chan struct{}          // signals abortGCLoop to stop

	// pendingMu guards pendingMsgs.
	pendingMu   sync.Mutex
	pendingMsgs map[string]*pendingRunQueue // sessionKey -> queued messages

	// runStateMachine tracks active agent runs for status broadcasting.
	runStateMachine *telegram.RunStateMachine

	// pluginHookRunner runs typed plugin hooks (before_model_resolve,
	// before_prompt_build, message_sending, etc.) during chat execution.
	pluginHookRunner *plugin.TypedHookRunner

	// hookRegistry fires user-defined shell hooks on message/tool events.
	hookRegistry *hooks.Registry

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
	LLMClient       *llm.Client
	Transcript      TranscriptStore
	Tools           *ToolRegistry
	AuthManager     *provider.AuthManager
	JobTracker      *agent.JobTracker
	ProviderConfigs map[string]ProviderConfig // provider ID → config
	AuroraStore     *aurora.Store             // Aurora hierarchical compaction store
	VegaBackend     vega.Backend              // optional; enables knowledge prefetch in chat
	MemoryStore     *memory.Store             // optional; structured memory (Honcho-style)
	SessionMemory   *SessionMemoryStore       // optional; structured session working state
	MemoryEmbedder  *memory.Embedder          // optional; fact embedding via SGLang
	UnifiedStore    *unified.Store            // optional; unified memory (search + tier-1)
	DreamTurnFn     func(ctx context.Context) // optional; increments dream turn via autonomous
	AgentLog        *agentlog.Writer          // optional; agent detail logging
	Registry        *modelrole.Registry       // centralized model role registry
	ContextCfg      ContextConfig
	CompactionCfg   CompactionConfig
	DefaultModel    string
	DefaultSystem   string
	MaxTokens       int
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		MaxHistoryBytes: 2 * 1024 * 1024, // 2 MB
		MaxHistoryCount: 200,
		MaxMessageBytes: 128 * 1024, // 128 KB
		ContextCfg:      DefaultContextConfig(),
		CompactionCfg:   DefaultCompactionConfig(),
		MaxTokens:       defaultMaxTokens,
	}
}

// NewHandler creates a new chat handler.
func NewHandler(sessions *session.Manager, broadcast BroadcastFunc, logger *slog.Logger, cfg HandlerConfig) *Handler {
	if cfg.MaxHistoryBytes == 0 {
		defaults := DefaultHandlerConfig()
		cfg.MaxHistoryBytes = defaults.MaxHistoryBytes
		cfg.MaxHistoryCount = defaults.MaxHistoryCount
		cfg.MaxMessageBytes = defaults.MaxMessageBytes
	}
	h := &Handler{
		sessions:        sessions,
		broadcast:       broadcast,
		logger:          logger,
		llmClient:       cfg.LLMClient,
		transcript:      cfg.Transcript,
		tools:           cfg.Tools,
		authManager:     cfg.AuthManager,
		jobTracker:      cfg.JobTracker,
		providerConfigs: cfg.ProviderConfigs,
		auroraStore:     cfg.AuroraStore,
		vegaBackend:     cfg.VegaBackend,
		memoryStore:     cfg.MemoryStore,
		sessionMemory:   cfg.SessionMemory,
		memoryEmbedder:  cfg.MemoryEmbedder,
		unifiedStore:    cfg.UnifiedStore,
		dreamTurnFn:     cfg.DreamTurnFn,
		agentLog:        cfg.AgentLog,
		registry:        cfg.Registry,
		contextCfg:      cfg.ContextCfg,
		compactionCfg:   cfg.CompactionCfg,
		defaultModel:    cfg.DefaultModel,
		defaultSystem:   cfg.DefaultSystem,
		maxTokens:       cfg.MaxTokens,
		abortMap:        make(map[string]*AbortEntry),
		pendingMsgs:     make(map[string]*pendingRunQueue),
		uploadLimits:    make(map[string]int64),
		done:            make(chan struct{}),
		maxHistoryBytes: cfg.MaxHistoryBytes,
		maxHistoryCount: cfg.MaxHistoryCount,
		maxMessageBytes: cfg.MaxMessageBytes,
	}
	// Set the package-level model role registry for sglang hooks and pilot tools.
	if h.registry != nil {
		SetModelRoleRegistry(h.registry)
	}
	go h.abortGCLoop()
	return h
}


// SetBroadcastRaw sets the raw broadcast function for streaming event relay.
func (h *Handler) SetBroadcastRaw(fn streaming.BroadcastRawFunc) {
	h.broadcastRaw = fn
}

// SetReplyFunc sets the function that delivers assistant responses back to the
// originating channel (e.g., Telegram). Called after each successful agent run
// when a DeliveryContext is present.
func (h *Handler) SetReplyFunc(fn ReplyFunc) {
	h.replyFunc = fn
}

// SetMediaSendFunc sets the function that delivers files back to the
// originating channel (e.g., Telegram). Used by the send_file tool.
func (h *Handler) SetMediaSendFunc(fn MediaSendFunc) {
	h.mediaSendFn = fn
}

// SetTypingFunc sets the function that sends typing indicators to the
// originating channel (e.g., Telegram "typing..." status) during agent runs.
func (h *Handler) SetTypingFunc(fn TypingFunc) {
	h.typingFn = fn
}

// SetReactionFunc sets the function that manages emoji reactions on the
// triggering message to indicate agent status phases (thinking, tool use, done).
func (h *Handler) SetReactionFunc(fn ReactionFunc) {
	h.reactionFn = fn
}

// SetRemoveReactionFunc sets the function that removes an emoji reaction
// from the triggering message when channel adapters require explicit cleanup.
func (h *Handler) SetRemoveReactionFunc(fn ReactionFunc) {
	h.removeReactionFn = fn
}

// SetToolProgressFunc sets the function that reports tool execution events
// (start/complete) to the originating channel for real-time progress display.
func (h *Handler) SetToolProgressFunc(fn ToolProgressFunc) {
	h.toolProgressFn = fn
}

// ToolProgressFunc returns the current tool progress function (for chaining).
func (h *Handler) ToolProgressFunc() ToolProgressFunc {
	return h.toolProgressFn
}

// SetDraftEditFunc sets the function that sends/edits streaming draft messages
// on the originating channel for real-time LLM output display.
func (h *Handler) SetDraftEditFunc(fn DraftEditFunc) {
	h.draftEditFn = fn
}

// SetDraftDeleteFunc sets the function that deletes streaming draft messages
// on the originating channel, used to clean up partial drafts before the final reply.
func (h *Handler) SetDraftDeleteFunc(fn DraftDeleteFunc) {
	h.draftDeleteFn = fn
}

// SetEmitAgentFunc sets the callback that sends agent lifecycle events
// (run.start, tool.start, tool.end) to the gateway event subscription pipeline.
func (h *Handler) SetEmitAgentFunc(fn func(kind, sessionKey, runID string, payload map[string]any)) {
	h.emitAgentFn = fn
}

// SetEmitTranscriptFunc sets the callback that sends transcript updates
// (user/assistant message appends) to the gateway event subscription pipeline.
func (h *Handler) SetEmitTranscriptFunc(fn func(sessionKey string, message any, messageID string)) {
	h.emitTranscriptFn = fn
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
	return h.removeReactionFn
}

// ReplyFunc returns the current reply function (for chaining).
func (h *Handler) ReplyFunc() ReplyFunc {
	return h.replyFunc
}

// MediaSendFunc returns the current media send function (for chaining).
func (h *Handler) MediaSendFunc() MediaSendFunc {
	return h.mediaSendFn
}

// TypingFunc returns the current typing function (for chaining).
func (h *Handler) TypingFunc() TypingFunc {
	return h.typingFn
}

// ReactionFunc returns the current reaction function (for chaining).
func (h *Handler) ReactionFunc() ReactionFunc {
	return h.reactionFn
}

// SetProviderRuntime sets the provider runtime resolver for runtime auth
// and missing-auth message generation during LLM client resolution.
func (h *Handler) SetProviderRuntime(pr *provider.ProviderRuntimeResolver) {
	h.providerRuntime = pr
}

// SetShutdownCtx sets the server lifecycle context so background goroutines
// (e.g., auto-memory extraction) are cancelled when the server shuts down.
func (h *Handler) SetShutdownCtx(ctx context.Context) {
	h.shutdownCtx = ctx
}

// SetRunStateMachine sets the state machine that tracks active agent runs.
func (h *Handler) SetRunStateMachine(sm *telegram.RunStateMachine) {
	h.runStateMachine = sm
}

// SetPluginHookRunner sets the typed hook runner for plugin lifecycle events.
func (h *Handler) SetPluginHookRunner(r *plugin.TypedHookRunner) {
	h.pluginHookRunner = r
}

// PluginHookRunner returns the typed hook runner (may be nil).
func (h *Handler) PluginHookRunner() *plugin.TypedHookRunner {
	return h.pluginHookRunner
}

// SetHookRegistry sets the user-defined hook registry for message/tool events.
func (h *Handler) SetHookRegistry(r *hooks.Registry) {
	h.hookRegistry = r
}

// DefaultModel returns the configured default LLM model name.
func (h *Handler) DefaultModel() string {
	return h.defaultModel
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
}
