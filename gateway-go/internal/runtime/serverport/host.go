// Package serverport is the leaf dependency-inversion boundary between the
// composition root (runtime/server) and the feature composition packages
// (runtime/servermail, runtime/serverchat, runtime/serverauto).
//
// It defines Host: the narrow set of cross-cutting accessors a feature
// package needs to reach state owned by ITS SIBLINGS or by the composition
// root itself, without importing runtime/server (which would create an
// import cycle: server -> serverchat -> server).
//
// *server.Server implements Host (see server/host.go); serverchat,
// servermail, and serverauto each hold a serverport.Host and call through
// it for anything they do not own outright. Feature packages must never
// import runtime/server.
package serverport

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/sparkfleet"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// Host is implemented by *server.Server (see server/host.go). It exposes
// every piece of composition-root or sibling-owned state that servermail,
// serverchat, and serverauto need but do not own themselves, so those
// packages never import runtime/server directly.
type Host interface {
	// --- Runtime context (composition root) ---
	Logger() *slog.Logger
	ShutdownCtx() context.Context
	DenebDir() string
	Activity() *monitoring.ActivityTracker
	SafeGo(name string, fn func())
	Broadcaster() *events.Broadcaster
	Processes() *process.Manager
	Fleet() *sparkfleet.Client
	Insights() *insights.Engine
	LogCapture() *observe.LogCapture
	PushHub() *proactive.Hub
	PushNotifier() *push.Notifier
	Version() string
	StartedAt() time.Time

	// --- RPC / provider (composition root, ServerRPC/ServerRuntime) ---
	Dispatcher() *rpc.Dispatcher
	GatewaySubs() *events.GatewayEventSubscriptions
	AuthManager() *provider.AuthManager
	ProviderRuntime() *provider.ProviderRuntimeResolver

	// --- Workflow / infra singletons (composition root) ---
	Approvals() *approval.Store
	JobTracker() *agent.JobTracker
	UsageTracker() *usage.Tracker
	MarketCache() *market.Cache
	MaintRunner() *maintenance.Runner
	// PromptOverride/PersonaOverride/MailAnalysisPrompt read the composition
	// root's operator-editable prompt store (server/prompt_store.go).
	PromptOverride(id string) (string, bool)
	PersonaOverride() string
	MailAnalysisPrompt() string

	// --- Memory (owned by servermail) ---
	WikiStore() *wiki.Store
	NotebookStore() *notebook.Store
	ContactsStore() *contacts.Store
	MailStore() *mailstore.Store
	WorkFeedStore() *workfeed.Store
	NativeSyncStore() *nativesync.Store
	CPProjects() *mailflow.CounterpartyProjectsCache
	NativeWorkFeedAppender() tooldeps.WorkFeedRW
	ProjectCandidatesFn() func() []mailanalysis.ProjectCandidate
	MailAnalysisModels() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string)

	// --- Chat (owned by serverchat) ---
	ChatHandler() *chat.Handler
	ToolDeps() *chat.CoreToolDeps
	ModelRegistry() *modelrole.Registry
	ProactiveRelay() proactive.Relay
	Sessions() *session.Manager
	// ResumableSessionForMarker classifies a run-marker session key as
	// resumable (native client sessions only) — wraps serverchat's session-key
	// classification so serverauto's auto-resume lane does not need to import
	// serverchat directly.
	ResumableSessionForMarker(sessionKey string) (channel string, ok bool)
	CronService() *cron.Service
	ExternalMCPClient(name string) *mcpclient.Client
	FileSemindex() *filesemindex.Service
	PolarisStore() *polaris.Store
	// SetGmailPollSvc wires the mail-poll service (built during chat init)
	// into serverauto's AutonomousSubsystem, which owns its lifecycle
	// (RegisterTask) — the one write serverchat needs to make into a
	// sibling-owned field.
	SetGmailPollSvc(svc *mailanalysis.Service)

	// --- Auto / Genesis (owned by serverauto) ---
	AutonomousSvc() *autonomous.Service
	GenesisTracker() *skilllifecycle.Tracker
	GenesisEvolver() *skilllifecycle.Evolver
	GenesisMeta() *skilllifecycle.MetaArtifacts
	SkillCatalog() *skilllifecycle.Catalog
	GenesisTranscripts() chat.TranscriptStore
	SetGenesisTranscripts(store chat.TranscriptStore)
	AgentLogWriter() *agentlog.Writer
	SetAgentLogWriter(w *agentlog.Writer)
	RoleHealth() *rolehealth.Watch
	ModelMaintenance() *modelmaintenance.Suite
	WikiDreamer() *wiki.WikiDreamer
	// SetWikiDreamer wires the dreamer built during serverchat's model-registry
	// init (it needs the lightweight-role client) into serverauto's
	// AutonomousSubsystem, which owns its lifecycle.
	SetWikiDreamer(d *wiki.WikiDreamer)

	// --- Feed cards implemented by servermail, called from serverauto ---
	PostLowConfidenceEvolveCard(result genesis.EvolveResult)
	PostMetaProposalCard(artifact, epoch, reason, path string, adopted bool)
	PostMetaRevertedCard(artifact, reason string)
	PostDriftFreezeCard(frozen bool, reasons []string)
	PostLadderReadyCard(title, detail string) error
	PostGraduationCard(key, title, evidence string)
	PostDreamWorkfeedCard(r *autonomous.DreamReport)
}
