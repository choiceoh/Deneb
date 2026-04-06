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

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rl"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
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

	// Channel plugins (Telegram is late-bound via SetTelegram).
	InternalHooks *hooks.InternalRegistry // nil-safe

	// Agent pipeline (Chat is late-bound via SetChat).
	Agents     *agent.Store
	JobTracker *agent.JobTracker

	// Scheduling.
	CronService    *cron.Service
	CronPersistLog *cron.PersistentRunLog // optional

	// Background task control plane.
	Tasks *tasks.Registry

	// Workflow subsystems.
	Approvals *approval.Store
	Skills    *skill.Manager
	Wizard    *wizard.Engine
	Talk      *talk.State // optional

	// RL self-learning (optional, nil when rl.enable=false).
	RLService *rl.Service

	// RLM context externalization (optional, nil when DENEB_RLM_ENABLED=false).
	RLMService *rlm.Service

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
	telegram      *telegram.Plugin // nil until SetTelegram (early phase).
	internalHooks *hooks.InternalRegistry // nil-safe

	// Agent pipeline.
	chat       *chat.Handler // nil until SetChat (late phase).
	agents     *agent.Store
	jobTracker *agent.JobTracker

	// local AI hub — centralized local LLM request management.
	localAIHub *localai.Hub

	// Scheduling.
	cronService    *cron.Service
	cronPersistLog *cron.PersistentRunLog

	// Background task control plane.
	tasks *tasks.Registry

	// Workflow subsystems.
	approvals *approval.Store
	skills    *skill.Manager
	wizard    *wizard.Engine
	talk      *talk.State

	// RL self-learning pipeline (optional, nil when rl is disabled).
	rlService *rl.Service

	// RLM context externalization (optional, nil when RLM is disabled).
	rlmService *rlm.Service

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
		internalHooks:  cfg.InternalHooks,
		agents:         cfg.Agents,
		jobTracker:     cfg.JobTracker,
		cronService:    cfg.CronService,
		cronPersistLog: cfg.CronPersistLog,
		tasks:          cfg.Tasks,
		approvals:      cfg.Approvals,
		skills:         cfg.Skills,
		wizard:         cfg.Wizard,
		talk:           cfg.Talk,
		rlService:      cfg.RLService,
		rlmService:     cfg.RLMService,
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
func (h *GatewayHub) InternalHooks() *hooks.InternalRegistry         { return h.internalHooks }
func (h *GatewayHub) Chat() *chat.Handler                            { return h.chat }
func (h *GatewayHub) Agents() *agent.Store                           { return h.agents }
func (h *GatewayHub) JobTracker() *agent.JobTracker                  { return h.jobTracker }
func (h *GatewayHub) CronService() *cron.Service                     { return h.cronService }
func (h *GatewayHub) CronPersistLog() *cron.PersistentRunLog         { return h.cronPersistLog }
func (h *GatewayHub) Tasks() *tasks.Registry                         { return h.tasks }
func (h *GatewayHub) Approvals() *approval.Store                     { return h.approvals }
func (h *GatewayHub) Skills() *skill.Manager                         { return h.skills }
func (h *GatewayHub) Wizard() *wizard.Engine                         { return h.wizard }
func (h *GatewayHub) Talk() *talk.State                              { return h.talk }
func (h *GatewayHub) RLService() *rl.Service                         { return h.rlService }
func (h *GatewayHub) RLMService() *rlm.Service                      { return h.rlmService }
func (h *GatewayHub) WikiStore() *wiki.Store                         { return h.wikiStore }
func (h *GatewayHub) Logger() *slog.Logger                           { return h.logger }
func (h *GatewayHub) Version() string                                { return h.version }
func (h *GatewayHub) LocalAIHub() *localai.Hub                         { return h.localAIHub }

// --- Late-binding setters ---

// SetLocalAIHub sets the centralized local AI hub (created early, before method registration).
func (h *GatewayHub) SetLocalAIHub(sh *localai.Hub) { h.localAIHub = sh }

// SetRLService sets the RL training service (optional, created during server init).
func (h *GatewayHub) SetRLService(s *rl.Service) { h.rlService = s }

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
	if h.agents == nil {
		missing = append(missing, "Agents")
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
	if h.wizard == nil {
		missing = append(missing, "Wizard")
	}
	if h.logger == nil {
		missing = append(missing, "Logger")
	}
	// Optional (nil-safe or late-bound):
	//   InternalHooks — explicitly nil-safe in handlers
	//   CronPersistLog — optional run log
	//   Talk — optional workflow state
	//   Telegram — late-bound via SetTelegram
	//   Chat — late-bound via SetChat
	//   Version — empty string is valid

	if len(missing) > 0 {
		return fmt.Errorf("GatewayHub missing required fields: %v", missing)
	}
	return nil
}
