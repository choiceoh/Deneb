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
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Handler manages chat RPC methods.
type Handler struct {
	// Embedded channel callbacks — all Set*/Get methods are promoted.
	*ChannelCallbacks

	sessions       *session.Manager
	broadcast      BroadcastFunc
	logger         *slog.Logger
	recordActivity func(sessionKey string)

	// Native agent execution deps.
	llmClient         *llm.Client
	transcript        TranscriptStore
	tools             *ToolRegistry
	authManager       *provider.AuthManager
	jobTracker        *agent.JobTracker
	providerConfigsMu sync.RWMutex
	providerConfigs   map[string]ProviderConfig
	embeddingClient   compact.Embedder                  // optional; BGE-M3 for MMR compaction fallback
	wikiStore         *wiki.Store                       // optional; wiki knowledge base
	dreamTurnFn       func(ctx context.Context)         // optional; increments dream turn via autonomous
	agentLog          *agentlog.Writer                  // optional; agent detail logging
	registry          *modelrole.Registry               // centralized model role registry
	providerRuntime   *provider.ProviderRuntimeResolver // optional; runtime auth, missing-auth messages

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

	// skillUsageRecorder records, per turn, which skills the agent consulted
	// and whether the turn succeeded — feeding the genesis Evolver real
	// success-rate data instead of empty stats. Optional; nil disables usage
	// attribution. Injected by the server via SetSkillUsageRecorder so chat
	// doesn't depend on the domain/skills/genesis package directly.
	skillUsageRecorder SkillUsageRecorder

	// topicResolver maps a forum threadID to a per-topic knowledge key for
	// system-prompt injection. Optional: nil disables per-topic knowledge.
	topicResolver TopicResolver

	// calendarGlanceFn builds the ambient upcoming-events glance injected into
	// the dynamic system-prompt block. Optional: nil disables ambient calendar
	// awareness (the live `calendar` tool is unaffected).
	calendarGlanceFn CalendarGlanceFunc

	// personaOverrideFn returns the operator-edited 업무 persona text (Settings
	// prompt corner). Optional: nil → default persona always renders.
	personaOverrideFn PersonaOverrideFunc

	// fileRecallFn runs a hybrid semantic search over the file store for the
	// recall preflight. Optional: nil disables the files recall source.
	fileRecallFn FileRecallFunc

	// weeklyReportTextFn / weeklyFormDeliverFn back the interactive /weekly
	// (/주간보고) slash command — the deterministic 주간업무보고 generators the
	// Saturday cron uses, so a manual trigger produces the same form + text.
	// Optional: nil → the command replies it is not wired. Injected by the
	// server via SetWeeklyReport so chat stays free of the wiki/render infra.
	weeklyReportTextFn  WeeklyReportTextFunc
	weeklyFormDeliverFn WeeklyFormDeliverFunc
}

// TopicResolver maps a forum/topic threadID to a per-topic knowledge key
// (from deneb.json topics.map). The concrete implementation lives in the
// server package and snapshots config at boot; the chat package stays free of
// any infra/config import by talking through this interface. nil disables
// per-topic knowledge injection.
type TopicResolver interface {
	// TopicKey returns the topic key for a threadID, or "" if unmapped. The
	// General topic (empty threadID) is normalized to "0". Must be cheap (an
	// in-memory map lookup) — it runs on the agent goroutine each turn.
	TopicKey(threadID string) string
	// Dir returns the configured knowledge directory (TopicsConfig.Dir); may
	// be empty, in which case the loader applies the "topics" default.
	Dir() string
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

// SkillUsageRecorder is the chat-side interface the server's genesis.Tracker
// satisfies (via adapter). The run loop calls it per turn to record that the
// agent consulted a skill and whether that turn succeeded; this populates the
// usage stats the Evolver's SkillsNeedingEvolution(minUses, maxSuccessRate)
// gate reads, so Propus converges on skills that actually
// fail rather than evolving blind. Keeps chat free of any domain import.
type SkillUsageRecorder interface {
	// RecordSkillUse logs one skill-consult outcome. Must not block — the chat
	// pipeline calls it synchronously on the run goroutine (the genesis
	// implementation does a cheap JSONL append).
	RecordSkillUse(sessionKey, skillName string, success bool, errMsg string)
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

	// TopicResolver maps a forum threadID to a per-topic knowledge key.
	// Optional: nil disables per-topic knowledge injection.
	TopicResolver TopicResolver

	// CalendarGlanceFn builds the ambient upcoming-events glance for the dynamic
	// system-prompt block. Optional: nil disables ambient calendar awareness.
	CalendarGlanceFn CalendarGlanceFunc

	// PersonaOverrideFn returns the operator-edited 업무 persona text (Settings
	// prompt corner), or "" when unedited. Optional: nil → default persona.
	PersonaOverrideFn PersonaOverrideFunc

	// FileRecallFn runs a hybrid semantic search over the on-box file store for
	// the recall preflight, so relevant uploaded files surface as recall evidence
	// alongside wiki/diary/session. Optional: nil disables the files recall source
	// (recall degrades to the other backends). Injected by the server closing over
	// the shared file semantic index.
	FileRecallFn FileRecallFunc

	// RecordActivity is called for user-originating chat turns so the server
	// can remember the latest active channel session for autonomous follow-ups.
	// The server owns filtering; chat only reports the session key.
	RecordActivity func(sessionKey string)
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
		recordActivity:       cfg.RecordActivity,
		llmClient:            cfg.LLMClient,
		transcript:           cfg.Transcript,
		tools:                cfg.Tools,
		authManager:          cfg.AuthManager,
		jobTracker:           cfg.JobTracker,
		topicResolver:        cfg.TopicResolver,
		calendarGlanceFn:     cfg.CalendarGlanceFn,
		personaOverrideFn:    cfg.PersonaOverrideFn,
		fileRecallFn:         cfg.FileRecallFn,
		providerConfigs:      cloneProviderConfigs(cfg.ProviderConfigs),
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
	// Cascade cleanup: when a parent session is killed or deleted, interrupt and
	// kill its running children. Subscribed for the handler's lifetime (same as
	// the notifier above).
	StartSubagentCleanup(SubagentCleanupDeps{
		Logger:       h.logger,
		Sessions:     func() *session.Manager { return h.sessions },
		InterruptRun: h.abort.InterruptSession,
	})
	return h
}

func cloneProviderConfigs(src map[string]ProviderConfig) map[string]ProviderConfig {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]ProviderConfig, len(src))
	for k, v := range src {
		if len(v.Headers) > 0 {
			headers := make(map[string]string, len(v.Headers))
			for hk, hv := range v.Headers {
				headers[hk] = hv
			}
			v.Headers = headers
		}
		out[k] = v
	}
	return out
}

// SetProviderConfigs replaces the runtime provider config snapshot used by
// future agent runs. Active runs keep the snapshot they already started with.
func (h *Handler) SetProviderConfigs(configs map[string]ProviderConfig) {
	h.providerConfigsMu.Lock()
	h.providerConfigs = cloneProviderConfigs(configs)
	h.providerConfigsMu.Unlock()
}

// ProviderConfigs returns a copy of the current runtime provider configs.
func (h *Handler) ProviderConfigs() map[string]ProviderConfig {
	h.providerConfigsMu.RLock()
	defer h.providerConfigsMu.RUnlock()
	return cloneProviderConfigs(h.providerConfigs)
}

// ModelRegistry returns the centralized model role registry.
func (h *Handler) ModelRegistry() *modelrole.Registry {
	return h.registry
}

// ToolNames returns the sorted names of the agent's registered tools, for
// callers that need the active toolset outside a turn (e.g. the Settings skills
// list, to populate eligibility's AvailableTools so requires_tools skills are
// filtered the same way the prompt and slash routing filter them). Returns nil
// if the handler or its registry is unset.
func (h *Handler) ToolNames() []string {
	if h == nil || h.tools == nil {
		return nil
	}
	return h.tools.SortedNames()
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

// SetSkillUsageRecorder installs the per-turn skill usage recorder. Pass nil
// to disable usage attribution. Safe to call before the first run starts.
func (h *Handler) SetSkillUsageRecorder(r SkillUsageRecorder) {
	h.skillUsageRecorder = r
}

// WeeklyReportTextFunc composes the deterministic 주간업무보고 text straight from
// wiki data (no LLM turn, so the format never drifts).
type WeeklyReportTextFunc func(ctx context.Context) (string, error)

// WeeklyFormDeliverFunc renders the formal 주간업무보고 form image and posts it to
// the native chat (best-effort; skipped when render is unavailable).
type WeeklyFormDeliverFunc func(ctx context.Context) error

// SetWeeklyReport wires the deterministic weekly-report generators so an
// interactive /weekly (/주간보고) slash command produces the same form image +
// text the Saturday cron does. nil fns leave the command gracefully unwired.
func (h *Handler) SetWeeklyReport(textFn WeeklyReportTextFunc, formFn WeeklyFormDeliverFunc) {
	h.weeklyReportTextFn = textFn
	h.weeklyFormDeliverFn = formFn
}

// RegisterTool installs a runtime-bound tool after handler construction.
// Used for subsystems, such as skill genesis, whose dependencies are created
// after the core chat tool registry is initialized.
func (h *Handler) RegisterTool(def ToolDef) bool {
	if h == nil || h.tools == nil || def.Name == "" || def.Fn == nil {
		return false
	}
	h.tools.RegisterTool(def)
	return true
}
