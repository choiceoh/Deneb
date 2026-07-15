// Package serverauto is the composition-root feature package for background
// autonomous services: the dreaming/task scheduler, Gmail polling lifecycle,
// role health, model maintenance, run-marker auto-resume, and the Genesis
// skill-lifecycle subsystem (generation, evolution, curriculum, meta
// evolution). It never imports runtime/server — cross-cutting state it needs
// from its siblings (mail, chat) comes through serverport.Host.
package serverauto

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	runtimemeeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/sessionstore"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// Manager owns the autonomous/dreaming service, run-marker auto-resume, and
// the Genesis skill-lifecycle subsystem (merged from the legacy
// AutonomousSubsystem + GenesisSubsystem). Its exported fields are set as
// each piece comes online during composition-root boot and read directly by
// the composition root — only lateral reads/writes from servermail/serverchat
// go through serverport.Host.
type Manager struct {
	Host serverport.Host

	AutonomousSvc *autonomous.Service
	// WikiDreamer is set during serverchat's model-registry init (it needs the
	// lightweight-role client), injected via Host.SetWikiDreamer since this
	// subsystem owns its lifecycle (SetDreamer starts the timer loop).
	WikiDreamer *wiki.WikiDreamer
	// GmailPollSvc is built during serverchat's chat config assembly but this
	// subsystem owns its lifecycle (RegisterTask), injected via Host.SetGmailPollSvc.
	GmailPollSvc     *mailanalysis.Service
	RoleHealth       *rolehealth.Watch
	ModelMaintenance *modelmaintenance.Suite

	// AgentLogWriter is the shared behavioral event log (the same instance the
	// chat pipeline uses), injected via Host.SetAgentLogWriter during
	// serverchat's RegisterSessionRPCMethods.
	AgentLogWriter *agentlog.Writer

	// Genesis skill-lifecycle. Concrete leaf types (generation/review) are
	// reached through skilllifecycle aliases so this composition-root package
	// does not import those leaves.
	GenesisSvc     *skilllifecycle.GenesisService
	GenesisMeta    *skilllifecycle.MetaArtifacts // RSI P1 prompt artifacts
	GenesisTracker *skilllifecycle.Tracker
	GenesisEvolver *skilllifecycle.Evolver
	GenesisNudger  *skilllifecycle.Nudger
	SkillCatalog   *skilllifecycle.Catalog
	// GenesisTranscripts is set from serverchat's session RPC init (the
	// transcript store chat owns), injected via Host.SetGenesisTranscripts.
	GenesisTranscripts toolport.TranscriptStore

	// Run-marker auto-resume: the marker store persists "run active at T"
	// records across gateway restarts (auto_resume.go). resumeMu guards
	// markerStore's lazy init; RunMarkerUnsub tears down the lifecycle
	// listener on shutdown (invoked by the composition root).
	resumeMu       sync.Mutex
	markerStore    *sessionstore.RunMarkerStore
	RunMarkerUnsub func()

	// Meeting/calendar/Plaud integration services (server_workflow_capabilities.go).
	// Referenced only within this package — the composition root starts them
	// via RegisterMeetingWorkflows and never reads them back.
	calendarBriefing *runtimemeeting.CalendarBriefingService
	meetingHarvest   *runtimemeeting.HarvestService
	plaudRecordings  *runtimemeeting.PlaudService
}

// New creates a Manager bound to host.
func New(host serverport.Host) *Manager {
	return &Manager{Host: host}
}
