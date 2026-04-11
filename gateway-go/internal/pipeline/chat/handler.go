package chat

import (
	"context"
	"log/slog"
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
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Handler manages chat RPC methods.
type Handler struct {
	// Embedded channel callbacks — all Set*/Get methods are promoted.
	*ChannelCallbacks

	sessions  *session.Manager
	broadcast BroadcastFunc
	logger    *slog.Logger

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
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int

	// Extracted components.
	abort    *AbortTracker
	pending  *PendingQueue
	subagent *SubagentNotifier

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
	BroadcastRaw streaming.BroadcastRawFunc // optional; raw event relay
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

	cb := NewChannelCallbacks(cfg.DefaultModel)
	// Initialize callbacks available at construction time.
	if cfg.BroadcastRaw != nil {
		cb.broadcastRaw = cfg.BroadcastRaw
	}
	if cfg.EmitAgentFn != nil {
		cb.emitAgentFn = cfg.EmitAgentFn
	}
	if cfg.EmitTranscriptFn != nil {
		cb.emitTranscriptFn = cfg.EmitTranscriptFn
	}

	h := &Handler{
		ChannelCallbacks:     cb,
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
		subagentDefaultModel: cfg.SubagentDefaultModel,
		defaultSystem:        cfg.DefaultSystem,
		maxTokens:            cfg.MaxTokens,
		providerRuntime: cfg.ProviderRuntime,
		abort:           NewAbortTracker(),
		pending:              NewPendingQueue(),
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
	h.subagent = NewSubagentNotifier(SubagentNotifierDeps{
		Logger:       h.logger,
		HasActiveRun: h.abort.HasActiveRun,
		StartRun: func(reqID string, params RunParams, isSteer bool) {
			h.startAsyncRun(reqID, params, isSteer)
		},
		EnqueuePend: h.pending.Enqueue,
		Sessions:    func() *session.Manager { return h.sessions },
	})
	return h
}

// ModelRegistry returns the centralized model role registry.
func (h *Handler) ModelRegistry() *modelrole.Registry {
	return h.registry
}

// Close stops background goroutines and cancels all active abort entries.
func (h *Handler) Close() {
	h.abort.Close()
	h.pending.Reset()
	h.subagent.Reset()
}
