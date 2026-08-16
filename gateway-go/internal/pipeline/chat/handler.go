package chat

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
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
	// memory groups the memory/knowledge backends. See MemoryDeps.
	memory               MemoryDeps
	dreamTurnFn          func(ctx context.Context)                             // optional; increments dream turn via autonomous
	preferenceSignalFn   func()                                                // optional; notes a 선호-tagged diary capsule for accelerated dreaming
	deliverablePublisher func(text string) (bool, error)                       // optional; auto-publishes a document-analysis deliverable to the work feed
	translateThinking    func(ctx context.Context, text string) (string, bool) // optional; renders the 🧠 blockquote into Korean
	agentLog             *agentlog.Writer                                      // optional; agent detail logging
	registry             *modelrole.Registry                                   // centralized model role registry
	providerRuntime      *provider.ProviderRuntimeResolver                     // optional; runtime auth, missing-auth messages

	// Agent run configuration.
	contextCfg           ContextConfig
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int
	runLimits            RunLimits
	samplingSeed         *int64
	disableTier1Wiki     bool
	semanticNow          func() time.Time
	semanticTimezone     string
	workspaceDir         string
	promptWorkspaceDir   string
	briefcaseMode        bool
	auditSystemPrompt    func(sessionKey string, prompt []byte)

	// Extracted components.
	abort                *AbortTracker
	pending              *PendingQueue
	mergeWindow          *MergeWindowTracker
	subagent             *SubagentNotifier
	subagentCleanupUnsub func()
	steer                *SteerQueue // mid-run /steer notes for the main agent
	linkEnrichStart      LinkEnrichStart
	normalizeCardReply   func(text, sessionKey string, logger *slog.Logger) string
	reportCardHealth     func(text, sessionKey string, logger *slog.Logger)

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

	// skills groups the Propus/genesis skill-loop hooks. See SkillDeps.
	skills SkillDeps

	// ambient groups the ambient system-prompt context providers (topic
	// knowledge, calendar/goal glances, persona override). See AmbientDeps.
	ambient AmbientDeps

	// weeklyReportTextFn / weeklyFormDeliverFn back the interactive /weekly
	// (/주간보고) slash command — the deterministic 주간업무보고 generators the
	// Saturday cron uses, so a manual trigger produces the same form + text.
	// Optional: nil → the command replies it is not wired. Injected by the
	// server via SetWeeklyReport so chat stays free of the wiki/render infra.
	weeklyReportTextFn  WeeklyReportTextFunc
	weeklyFormDeliverFn WeeklyFormDeliverFunc
}

// MemoryDeps groups the memory/knowledge backends that flow HandlerConfig →
// Handler → runDeps unchanged (triple-mirror cleanup, cluster 2). All fields
// optional; nil disables the corresponding source.
type MemoryDeps struct {
	// Wiki is the wiki knowledge base (tier-1 injection, recall, diary).
	Wiki *tooldeps.WikiStore
	// Notebook is the notebook session-grounding source store.
	Notebook *tooldeps.NotebookStore
	// FileRecall runs a hybrid semantic search over the on-box file store for
	// the recall preflight (recall degrades to the other backends when nil).
	// Injected by the server closing over the shared file semantic index.
	FileRecall FileRecallFunc
	// Org loads the operator's org chart (조직도) for the recall preflight's org
	// source (org members/divisions named in a turn → their 부서 + 인물 page).
	// Injected by the server as org.Load; nil disables the org recall source.
	Org chatrecall.OrgLoader
	// Embedding is the embedding sidecar client for the MMR compaction
	// fallback tier.
	Embedding compact.Embedder
}

// SkillDeps groups the Propus/genesis skill-loop hooks, injected via
// SetSkillNudger/SetSkillUsageRecorder so chat doesn't depend on the
// domain/skills/genesis package directly. nil disables each hook.
type SkillDeps struct {
	// Nudger fires mid-session skill reviews every N tool calls.
	Nudger SkillNudger
	// UsageRecorder attributes each turn's outcome to the skills consulted.
	UsageRecorder SkillUsageRecorder
}

// AmbientDeps groups the ambient system-prompt context providers that flow
// HandlerConfig → Handler → runDeps unchanged. One field here replaces three
// mirrored declarations and two field-by-field copies (the triple-mirror debt
// from the 2026-07 pipeline audit). Every field is optional; nil disables it.
type AmbientDeps struct {
	// TopicResolver maps a forum threadID to a per-topic knowledge key for
	// system-prompt injection.
	TopicResolver TopicResolver
	// CalendarGlance builds the ambient upcoming-events glance for the
	// dynamic system-prompt block.
	CalendarGlance CalendarGlanceFunc
	// GoalGlance builds the ambient active-goal glance for the dynamic block.
	GoalGlance GoalGlanceFunc
	// PersonaOverride returns the operator-edited 업무 persona text (Settings
	// prompt corner); "" → the default persona renders. Read per turn —
	// byte-stable between rare edits, so the Static cache holds.
	PersonaOverride PersonaOverrideFunc
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

type (
	SkillNudger            = chatport.SkillNudger
	SkillNudgeSnapshot     = chatport.SkillNudgeSnapshot
	SkillNudgeToolActivity = chatport.SkillNudgeToolActivity
	SkillUsageRecorder     = chatport.SkillUsageRecorder
)

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
	// Memory groups the memory/knowledge backends. See MemoryDeps.
	Memory             MemoryDeps
	DreamTurnFn        func(ctx context.Context) // optional; increments dream turn via autonomous
	PreferenceSignalFn func()                    // optional; notes a 선호-tagged diary capsule so the dreamer consolidates it on the accelerated cadence
	// DeliverablePublisher files a document-analysis turn's final response as a
	// doc_analysis work-feed card (server-side auto safety net). Optional; nil disables.
	DeliverablePublisher func(text string) (bool, error)
	// TranslateThinking renders the turn's extended-thinking text into Korean
	// for the 🧠 blockquote. Display only — the streamed reasoning payload is
	// untouched. Optional; nil leaves thinking in the model's own language.
	TranslateThinking    func(ctx context.Context, text string) (string, bool)
	AgentLog             *agentlog.Writer    // optional; agent detail logging
	Registry             *modelrole.Registry // centralized model role registry
	ContextCfg           ContextConfig
	DefaultModel         string
	SubagentDefaultModel string // separate default model for sub-agents (from agents.defaults.subagents.model)
	DefaultSystem        string
	MaxTokens            int
	// RunLimits overrides the mode-derived agent budget for every run through
	// this handler. Zero fields preserve production defaults. It is handler-
	// scoped rather than a public per-request option so benchmark callers cannot
	// silently change the signed case policy mid-run.
	RunLimits RunLimits
	// SamplingSeed is a trusted handler-scoped OpenAI-compatible sampling seed.
	// Anthropic wire mode omits it; provider determinism is still best-effort.
	SamplingSeed *int64
	// DisableTier1Wiki prevents ambient high-importance pages from entering the
	// system prompt while leaving explicit recall and wiki tools available.
	DisableTier1Wiki bool
	// SemanticNow supplies user-visible wall-clock time for prompts, transcript
	// timestamps, and temporal recall. Nil uses Deneb's normal configured clock.
	// Latency and timeout measurement deliberately keep the real monotonic clock.
	SemanticNow func() time.Time
	// SemanticTimezone overrides the prompt timezone for this handler without
	// mutating process-global Deneb configuration. Empty uses the normal zone.
	SemanticTimezone string
	// WorkspaceDir binds prompt context and filesystem-facing harness adapters
	// to an explicit root. Empty preserves production workspace resolution.
	WorkspaceDir string
	// PromptWorkspaceDir is a stable display-only workspace label. Filesystem
	// operations still use WorkspaceDir. Empty displays the real path.
	PromptWorkspaceDir string
	// BriefcaseMode disables ambient production shortcuts that sit outside a
	// signed evaluation world.
	BriefcaseMode   bool
	LinkEnrichStart LinkEnrichStart
	// NormalizeCardReply is the final deneb-ui validity boundary before the
	// assistant reply is persisted or delivered. Optional; nil is a no-op.
	NormalizeCardReply func(text, sessionKey string, logger *slog.Logger) string
	ReportCardHealth   func(text, sessionKey string, logger *slog.Logger)
	// AuditSystemPrompt receives the exact finalized system-prompt wire bytes.
	// It is a trusted observability hook used by deterministic evaluation only.
	AuditSystemPrompt func(sessionKey string, prompt []byte)

	// Fields below were previously Set*() after construction. They are all
	// available at handler creation time and passed here to reduce late-binding.
	ProviderRuntime  *provider.ProviderRuntimeResolver // optional; runtime auth
	BroadcastRaw     streaming.BroadcastRawFunc        // optional; raw event relay
	EmitAgentFn      func(kind, sessionKey, runID string, payload jsonObject)
	EmitTranscriptFn func(sessionKey string, message rawJSON, messageID string)

	// Ambient groups the ambient system-prompt context providers (topic
	// knowledge, calendar/goal glances, persona override). See AmbientDeps.
	Ambient AmbientDeps

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
	if cfg.SemanticNow == nil {
		cfg.SemanticNow = dentime.Now
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
		ambient:              cfg.Ambient,
		providerConfigs:      cloneProviderConfigs(cfg.ProviderConfigs),
		memory:               cfg.Memory,
		dreamTurnFn:          cfg.DreamTurnFn,
		preferenceSignalFn:   cfg.PreferenceSignalFn,
		deliverablePublisher: cfg.DeliverablePublisher,
		translateThinking:    cfg.TranslateThinking,
		agentLog:             cfg.AgentLog,
		registry:             cfg.Registry,
		contextCfg:           cfg.ContextCfg,
		subagentDefaultModel: cfg.SubagentDefaultModel,
		defaultSystem:        cfg.DefaultSystem,
		maxTokens:            cfg.MaxTokens,
		runLimits:            cfg.RunLimits,
		samplingSeed:         cfg.SamplingSeed,
		disableTier1Wiki:     cfg.DisableTier1Wiki,
		semanticNow:          cfg.SemanticNow,
		semanticTimezone:     cfg.SemanticTimezone,
		workspaceDir:         cfg.WorkspaceDir,
		promptWorkspaceDir:   cfg.PromptWorkspaceDir,
		briefcaseMode:        cfg.BriefcaseMode,
		auditSystemPrompt:    cfg.AuditSystemPrompt,
		providerRuntime:      cfg.ProviderRuntime,
		abort:                NewAbortTracker(),
		pending:              NewPendingQueue(),
		mergeWindow:          NewMergeWindowTracker(),
		steer:                NewSteerQueue(),
		linkEnrichStart:      cfg.LinkEnrichStart,
		normalizeCardReply:   cfg.NormalizeCardReply,
		reportCardHealth:     cfg.ReportCardHealth,
		maxHistoryBytes:      cfg.MaxHistoryBytes,
		maxHistoryCount:      cfg.MaxHistoryCount,
		maxMessageBytes:      cfg.MaxMessageBytes,
	}
	// Set the package-level model role registry for local AI hooks.
	if h.registry != nil {
		pilot.SetModelRoleRegistry(h.registry)
	}
	// Wire centralized local AI hub for token budget management and health checks.
	h.subagent = NewSubagentNotifier(SubagentNotifierDeps{
		Logger:       h.logger,
		HasActiveRun: h.abort.HasActiveRun,
		StartRun: func(reqID string, params RunParams, isSteer bool) {
			h.startOrQueueRun(reqID, params, isSteer)
		},
		EnqueuePend: func(sessionKey string, params RunParams) {
			h.startOrQueueRun("subnotify-"+params.ClientRunID, params, false)
		},
		Sessions: func() *session.Manager { return h.sessions },
	})
	// Cascade cleanup: when a parent session is killed or deleted, interrupt and
	// kill its running children. Subscribed for the handler's lifetime (same as
	// the notifier above).
	h.subagentCleanupUnsub = StartSubagentCleanup(SubagentCleanupDeps{
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
	if h == nil {
		return
	}
	h.abort.Close()
	h.pending.Reset()
	h.mergeWindow.Reset()
	if h.subagent != nil {
		h.subagent.Close()
	}
	if h.subagentCleanupUnsub != nil {
		h.subagentCleanupUnsub()
		h.subagentCleanupUnsub = nil
	}
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

// SteerNative is the explicit mid-turn correction path for the native
// client (miniapp.chat.steer). Unlike auto-steer on SendSync, this does
// not start a turn and does not return an assistant ack — the in-flight
// stream keeps going. Returns false when there is no interactive run or
// the note is empty.
func (h *Handler) SteerNative(sessionKey, note string) bool {
	if h == nil {
		return false
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return false
	}
	if h.abort == nil || !h.abort.HasActiveInteractiveRun(sessionKey) {
		return false
	}
	if !h.EnqueueSteer(sessionKey, trimmed) {
		return false
	}
	if h.transcript != nil {
		now := dentime.Now()
		userMsg := NewTextChatMessage("user", formatTurnUserMessage(trimmed, now), now.UnixMilli())
		if err := h.transcript.Append(sessionKey, userMsg); err != nil {
			h.logger.Error("native-steer: persist steered user message failed", "sessionKey", sessionKey, "error", err)
		}
	}
	h.logger.Info("native-steer: folded mid-run note into the active run", "sessionKey", sessionKey, "runes", utf8.RuneCountInString(trimmed))
	return true
}

// SteerQueue returns the queue for internal wiring (used by runDeps to
// give the agent run goroutine access without leaking the Handler).
func (h *Handler) SteerQueue() *SteerQueue {
	return h.steer
}

// ActiveRunCount reports accepted-but-unfinished agent runs (streaming and
// sync alike) — the same population the shutdown drain waits for. /health
// exposes it so the deploy idle gate can wait for zero before a hot-swap.
func (h *Handler) ActiveRunCount() int {
	if h == nil || h.abort == nil {
		return 0
	}
	return h.abort.ActiveRunCount()
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
	h.skills.Nudger = n
}

// SetSkillUsageRecorder installs the per-turn skill usage recorder. Pass nil
// to disable usage attribution. Safe to call before the first run starts.
func (h *Handler) SetSkillUsageRecorder(r SkillUsageRecorder) {
	h.skills.UsageRecorder = r
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
