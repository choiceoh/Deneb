// GatewayHub is the central service container for the gateway server.
//
// It holds references to all shared services and stores. No business logic
// lives here — only Broadcast() (fan-out helper), Validate() (startup check),
// and phase tracking (initialization order safety).
//
// Built once in server.New() via NewGatewayHub(), passed to method registration.
// Handler packages never import this type; they receive Deps structs instead.
//
// Fields are private; read-only accessors are provided. The concrete chat
// pipeline remains owned by the server composition root instead of being
// duplicated in this RPC service container.
//
// Late-bound optional services (wiki/contacts/insights) live in Opt so this
// package's direct fan-out stays at the required core set.
package hub

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil/hub/opt"
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

	// Agent pipeline bookkeeping.
	JobTracker *agent.JobTracker

	// Scheduling.
	CronService    *cron.Service
	CronPersistLog *cron.PersistentRunLog // optional

	// Workflow subsystems.
	Approvals *approval.Store
	Skills    *skills.Registry

	// Metadata.
	Logger  *slog.Logger
	Version string // optional
}

// GatewayHub owns the shared runtime services wired into RPC handlers.
type GatewayHub struct {
	// Event infrastructure.
	broadcaster *events.Broadcaster
	gatewaySubs *events.GatewayEventSubscriptions

	// Session and process management.
	sessions  *session.Manager
	processes *process.Manager

	// Agent pipeline bookkeeping.
	jobTracker *agent.JobTracker

	// Scheduling.
	cronService    *cron.Service
	cronPersistLog *cron.PersistentRunLog

	// Workflow subsystems.
	approvals *approval.Store
	skills    *skills.Registry

	// Late-bound optional services (wiki/contacts/insights).
	Opt *opt.Services

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
		approvals:      cfg.Approvals,
		skills:         cfg.Skills,
		Opt:            &opt.Services{},
		logger:         cfg.Logger,
		version:        cfg.Version,
		phase:          PhaseInit,
	}
}

// --- Read-only accessors ---

// Broadcaster returns the gateway event broadcaster.
func (h *GatewayHub) Broadcaster() *events.Broadcaster { return h.broadcaster }

// GatewaySubs returns the lifecycle event subscription registry.
func (h *GatewayHub) GatewaySubs() *events.GatewayEventSubscriptions { return h.gatewaySubs }

// Sessions returns the session manager.
func (h *GatewayHub) Sessions() *session.Manager { return h.sessions }

// Processes returns the managed-process registry.
func (h *GatewayHub) Processes() *process.Manager { return h.processes }

// JobTracker returns the background agent job tracker.
func (h *GatewayHub) JobTracker() *agent.JobTracker { return h.jobTracker }

// CronService returns the scheduler service.
func (h *GatewayHub) CronService() *cron.Service { return h.cronService }

// CronPersistLog returns the optional durable cron run log.
func (h *GatewayHub) CronPersistLog() *cron.PersistentRunLog { return h.cronPersistLog }

// Approvals returns the approval request store.
func (h *GatewayHub) Approvals() *approval.Store { return h.approvals }

// Skills returns the runtime skill registry.
func (h *GatewayHub) Skills() *skills.Registry { return h.skills }

// Logger returns the gateway logger.
func (h *GatewayHub) Logger() *slog.Logger { return h.logger }

// Version returns the configured gateway build version.
func (h *GatewayHub) Version() string { return h.version }

// --- Broadcast ---

// Broadcast sends an event to all connected SSE clients.
// Satisfies BroadcastFunc signature for direct use in handler Deps.
func (h *GatewayHub) Broadcast(event string, payload events.EventPayload) (int, []error) {
	if h == nil || h.broadcaster == nil {
		return 0, []error{fmt.Errorf("gateway broadcaster is not available")}
	}
	return h.broadcaster.Broadcast(event, payload)
}

// --- Phase tracking ---

// AdvancePhase moves the hub to the target registration phase.
// Panics if the target is not exactly one step ahead of the current phase,
// preventing out-of-order initialization.
func (h *GatewayHub) AdvancePhase(target uint8) {
	if target > PhaseLate || target != h.phase+1 {
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
	if h == nil {
		return fmt.Errorf("gatewayHub is nil")
	}
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
	//   Opt.WikiStore / Opt.ContactsStore / Opt.Insights — late-bound
	//   CronPersistLog — optional run log
	//   Version — empty string is valid

	if len(missing) > 0 {
		return fmt.Errorf("gatewayHub missing required fields: %v", missing)
	}
	return nil
}
