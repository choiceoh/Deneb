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
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
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
	embeddingClient compact.Embedder                  // optional; BGE-M3 for MMR compaction fallback
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
	abort       *AbortTracker
	pending     *PendingQueue
	mergeWindow *MergeWindowTracker
	subagent    *SubagentNotifier
	steer       *SteerQueue // mid-run /steer notes for the main agent

	// checkpointRoot is the directory where per-session file-edit snapshots
	// are stored (e.g. "~/.deneb/checkpoints"). When non-empty, each agent
	// run lazily constructs a checkpoint.Manager scoped to its SessionKey
	// and attaches it to the run context so fs tools can snapshot before
	// mutating files. Empty string disables snapshotting (no-op on edits).
	checkpointRoot string

	// maxHistoryBytes caps the total JSON bytes returned by chat.history.
	maxHistoryBytes int
	// maxHistoryCount caps the number of messages returned.
	maxHistoryCount int
	// maxMessageBytes caps a single message body before truncation.
	maxMessageBytes int

	// skillNudger fires mid-session skill reviews every N tool calls.
	// Optional — when nil, the tool-call accounting path short-circuits.
	// Injected by the server via SetSkillNudger so chat doesn't depend
	// on the domain/skills/genesis package directly.
	skillNudger SkillNudger
}

// SkillNudger is the chat-side interface the server's genesis.Nudger
// satisfies. Keeps chat free of any domain import.
type SkillNudger interface {
	// Enabled reports whether the nudger is configured to fire.
	Enabled() bool
	// OnToolCalls is called after each turn with the number of tool
	// invocations that completed in that turn and a snapshot of the
	// session state. Implementations must not block — the chat pipeline
	// calls this synchronously on the run goroutine.
	OnToolCalls(ctx context.Context, sessionKey string, delta int, snapshot SkillNudgeSnapshot)
	// Reset clears the per-session counter; call on session end/abort.
	Reset(sessionKey string)
}

// SkillNudgeSnapshot is the minimal projection of an agent run the
// nudger needs to evaluate "is this skill-worthy?".
type SkillNudgeSnapshot struct {
	Turns          int
	ToolActivities []SkillNudgeToolActivity
	AllText        string
	Label          string
	Model          string
}

// SkillNudgeToolActivity mirrors the tool activity record the nudger
// uses. Intentionally decoupled from agent.ToolActivity so chat doesn't
// leak domain types outward.
type SkillNudgeToolActivity struct {
	Name    string
	IsError bool
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
	EmbeddingClient      compact.Embedder          // optional; BGE-M3 for MMR compaction fallback
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
	ProviderRuntime  *provider.ProviderRuntimeResolver // optional; runtime auth
	BroadcastRaw     streaming.BroadcastRawFunc        // optional; raw event relay
	EmitAgentFn      func(kind, sessionKey, runID string, payload map[string]any)
	EmitTranscriptFn func(sessionKey string, message any, messageID string)
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

// InsightsProviderFunc returns a Telegram-safe MarkdownV2 report string for the
// /insights command. `days` is the lookback window (defaulted upstream).
// Returns an error if generation fails — the dispatcher surfaces the failure
// to the user without leaking internals.
type InsightsProviderFunc func(ctx context.Context, days int) (markdown string, err error)

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
		embeddingClient:      cfg.EmbeddingClient,
		wikiStore:            cfg.WikiStore,
		dreamTurnFn:          cfg.DreamTurnFn,
		agentLog:             cfg.AgentLog,
		registry:             cfg.Registry,
		contextCfg:           cfg.ContextCfg,
		subagentDefaultModel: cfg.SubagentDefaultModel,
		defaultSystem:        cfg.DefaultSystem,
		maxTokens:            cfg.MaxTokens,
		providerRuntime:      cfg.ProviderRuntime,
		abort:                NewAbortTracker(),
		pending:              NewPendingQueue(),
		mergeWindow:          NewMergeWindowTracker(),
		steer:                NewSteerQueue(),
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
	h.mergeWindow.Reset()
	h.subagent.Reset()
	h.steer.Reset()
}

// EnqueueSteer queues a /steer note for injection into the active (or next)
// agent run for sessionKey. Returns true if the note was accepted.
//
// Used by the chat.steer RPC method and by the autoreply slash command
// dispatcher (main-agent /steer). A separate entry point from the subagent
// /steer path, which operates on a child-session run-id rather than on
// the caller's own running turn.
func (h *Handler) EnqueueSteer(sessionKey, note string) bool {
	if h.steer == nil {
		return false
	}
	return h.steer.Enqueue(sessionKey, note)
}

// SteerQueue returns the queue for internal wiring (used by runDeps to
// give the agent run goroutine access without leaking the Handler).
func (h *Handler) SteerQueue() *SteerQueue {
	return h.steer
}

// SetCheckpointRoot configures the directory under which file-edit snapshots
// are written (one subdirectory per SessionKey). Pass empty string to
// disable snapshotting entirely. Safe to call at any time; new runs pick up
// the latest value. Idempotent.
func (h *Handler) SetCheckpointRoot(dir string) {
	h.checkpointRoot = dir
}

// CheckpointRoot returns the configured snapshot root, or "" when disabled.
func (h *Handler) CheckpointRoot() string {
	return h.checkpointRoot
}

// SetSkillNudger installs the iteration-based skill nudger. Pass nil to
// disable. Safe to call before the first run starts; not expected to
// change at runtime.
func (h *Handler) SetSkillNudger(n SkillNudger) {
	h.skillNudger = n
}
