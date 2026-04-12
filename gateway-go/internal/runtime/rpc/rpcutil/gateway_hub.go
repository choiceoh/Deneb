// GatewayHub is the central service container for the gateway server.
//
// It holds references to all shared services and stores. No business logic
// lives here — only Broadcast() (fan-out helper), Validate() (startup check),
// and phase tracking (initialization order safety).
//
// Built once in server.New() via NewGatewayHub(), passed to method registration.
// Handler packages never import this type; they receive Deps structs instead.
//
// Fields are private; read-only accessors are provided. Only Telegram and Chat
// have setters (late-bound during registration phases).
package rpcutil

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Registration phase constants. Phases must advance in order:
// PhaseInit → PhaseEarly → PhaseSession → PhaseLate.
const (
	PhaseInit    uint8 = iota // Hub constructed, no methods registered yet.
	PhaseEarly                // registerEarlyMethods running.
	PhaseSession              // registerSessionRPCMethods completed (chatHandler created).
	PhaseLate                 // registerLateMethods running (Chat available).
)

// HubConfig holds the initial values for constructing a GatewayHub.
// All fields that are required by Validate() must be non-nil here.
type HubConfig struct {
	// Event infrastructure.
	Broadcaster *events.Broadcaster
	GatewaySubs *events.GatewayEventSubscriptions

	// Session and process management.
	Sessions  *session.Manager
	Processes *process.Manager

	// Agent pipeline (Chat is late-bound via SetChat).
	JobTracker *agent.JobTracker

	// Scheduling.
	CronService    *cron.Service
	CronPersistLog *cron.PersistentRunLog // optional

	// Background task control plane.
	Tasks *tasks.Registry

	// Workflow subsystems.
	Approvals *approval.Store
	Skills    *skills.Registry

	// Metadata.
	Logger  *slog.Logger
	Version string // optional
}

type GatewayHub struct {
	// Event infrastructure.
	broadcaster *events.Broadcaster
	gatewaySubs *events.GatewayEventSubscriptions

	// Session and process management.
	sessions  *session.Manager
	processes *process.Manager

	// Channel plugins.
	telegram *telegram.Plugin // nil until SetTelegram (early phase).

	// Agent pipeline.
	chat       *chat.Handler // nil until SetChat (late phase).
	jobTracker *agent.JobTracker

	// local AI hub — centralized local LLM request management.
	localAIHub *localai.Hub

	// embedding client — BGE-M3 for MMR-based compaction fallback.
	embeddingClient *embedding.Client

	// Scheduling.
	cronService    *cron.Service
	cronPersistLog *cron.PersistentRunLog

	// Background task control plane.
	tasks *tasks.Registry

	// Workflow subsystems.
	approvals *approval.Store
	skills    *skills.Registry

	// Wiki knowledge base (optional, nil when wiki is disabled).
	wikiStore *wiki.Store

	// Metadata.
	logger  *slog.Logger
	version string

	// Phase tracking for initialization order safety.
	phase uint8
}

// NewGatewayHub constructs a GatewayHub from the provided config.
// The hub starts in PhaseInit. Call AdvancePhase() to progress through
// registration phases.
func NewGatewayHub(cfg HubConfig) *GatewayHub {
	return &GatewayHub{
		broadcaster:    cfg.Broadcaster,
		gatewaySubs:    cfg.GatewaySubs,
		sessions:       cfg.Sessions,
		processes:      cfg.Processes,
		jobTracker:     cfg.JobTracker,
		cronService:    cfg.CronService,
		cronPersistLog: cfg.CronPersistLog,
		tasks:          cfg.Tasks,
		approvals:      cfg.Approvals,
		skills:         cfg.Skills,
		logger:         cfg.Logger,
		version:        cfg.Version,
		phase:          PhaseInit,
	}
}

// --- Read-only accessors ---

func (h *GatewayHub) Broadcaster() *events.Broadcaster               { return h.broadcaster }
func (h *GatewayHub) GatewaySubs() *events.GatewayEventSubscriptions { return h.gatewaySubs }
func (h *GatewayHub) Sessions() *session.Manager                     { return h.sessions }
func (h *GatewayHub) Processes() *process.Manager                    { return h.processes }
func (h *GatewayHub) Telegram() *telegram.Plugin                     { return h.telegram }
func (h *GatewayHub) Chat() *chat.Handler                            { return h.chat }
func (h *GatewayHub) JobTracker() *agent.JobTracker                  { return h.jobTracker }
func (h *GatewayHub) CronService() *cron.Service                     { return h.cronService }
func (h *GatewayHub) CronPersistLog() *cron.PersistentRunLog         { return h.cronPersistLog }
func (h *GatewayHub) Tasks() *tasks.Registry                         { return h.tasks }
func (h *GatewayHub) Approvals() *approval.Store                     { return h.approvals }
func (h *GatewayHub) Skills() *skills.Registry                       { return h.skills }
func (h *GatewayHub) WikiStore() *wiki.Store                         { return h.wikiStore }
func (h *GatewayHub) Logger() *slog.Logger                           { return h.logger }
func (h *GatewayHub) Version() string                                { return h.version }
func (h *GatewayHub) LocalAIHub() *localai.Hub                       { return h.localAIHub }
func (h *GatewayHub) EmbeddingClient() *embedding.Client             { return h.embeddingClient }

// --- Late-binding setters ---

// SetLocalAIHub sets the centralized local AI hub (created early, before method registration).
func (h *GatewayHub) SetLocalAIHub(sh *localai.Hub) { h.localAIHub = sh }

// SetEmbeddingClient sets the BGE-M3 embedding client for MMR compaction fallback.
func (h *GatewayHub) SetEmbeddingClient(c *embedding.Client) { h.embeddingClient = c }

// SetWikiStore sets the wiki knowledge base (optional, created during session phase).
func (h *GatewayHub) SetWikiStore(s *wiki.Store) { h.wikiStore = s }

// SetTelegram sets the Telegram plugin (created during early registration phase).
func (h *GatewayHub) SetTelegram(p *telegram.Plugin) { h.telegram = p }

// SetChat sets the chat handler. Panics if called before PhaseSession,
// ensuring the chat handler is actually created before being wired.
func (h *GatewayHub) SetChat(c *chat.Handler) {
	if h.phase < PhaseSession {
		panic("GatewayHub.SetChat called before PhaseSession — chatHandler not yet created")
	}
	h.chat = c
}

// --- Broadcast ---

// Broadcast sends an event to all connected WebSocket clients.
// Satisfies BroadcastFunc signature for direct use in handler Deps.
func (h *GatewayHub) Broadcast(event string, payload any) (int, []error) {
	return h.broadcaster.Broadcast(event, payload)
}

// --- Phase tracking ---

// AdvancePhase moves the hub to the target registration phase.
// Panics if the target is not exactly one step ahead of the current phase,
// preventing out-of-order initialization.
func (h *GatewayHub) AdvancePhase(target uint8) {
	if target != h.phase+1 {
		panic(fmt.Sprintf("GatewayHub.AdvancePhase: expected phase %d, got target %d (current: %d)",
			h.phase+1, target, h.phase))
	}
	h.phase = target
}

// Phase returns the current registration phase (for testing).
func (h *GatewayHub) Phase() uint8 { return h.phase }

// --- Validation ---

// Validate checks that all required hub fields are non-nil.
// Called once at startup before method registration begins.
func (h *GatewayHub) Validate() error {
	var missing []string

	// Required: used by handlers without nil checks.
	if h.broadcaster == nil {
		missing = append(missing, "Broadcaster")
	}
	if h.gatewaySubs == nil {
		missing = append(missing, "GatewaySubs")
	}
	if h.sessions == nil {
		missing = append(missing, "Sessions")
	}
	if h.processes == nil {
		missing = append(missing, "Processes")
	}
	if h.jobTracker == nil {
		missing = append(missing, "JobTracker")
	}
	if h.cronService == nil {
		missing = append(missing, "CronService")
	}
	if h.approvals == nil {
		missing = append(missing, "Approvals")
	}
	if h.skills == nil {
		missing = append(missing, "Skills")
	}
	if h.logger == nil {
		missing = append(missing, "Logger")
	}
	// Optional (nil-safe or late-bound):
	//   InternalHooks — explicitly nil-safe in handlers
	//   CronPersistLog — optional run log
	//   Telegram — late-bound via SetTelegram
	//   Chat — late-bound via SetChat
	//   Version — empty string is valid

	if len(missing) > 0 {
		return fmt.Errorf("gatewayHub missing required fields: %v", missing)
	}
	return nil
}
