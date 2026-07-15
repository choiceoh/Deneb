package server

import (
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
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverchat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// This file implements serverport.Host on *Server, the sole cycle-safe bridge
// that lets servermail/serverchat/serverauto reach composition-root or
// sibling-owned state without importing runtime/server. See
// docs/agent-rules/hub-wiring.md and serverport/host.go's package doc.

// --- Runtime context ---

func (s *Server) Logger() *slog.Logger                  { return s.logger }
func (s *Server) DenebDir() string                      { return s.denebDir }
func (s *Server) Activity() *monitoring.ActivityTracker { return s.activity }
func (s *Server) SafeGo(name string, fn func())         { s.safeGo(name, fn) }

// Broadcaster is implemented in server_monitoring.go.

func (s *Server) Processes() *process.Manager { return s.processes }
func (s *Server) Fleet() *sparkfleet.Client             { return s.fleet }
func (s *Server) Insights() *insights.Engine            { return s.insights }
func (s *Server) LogCapture() *observe.LogCapture       { return s.logCapture }
func (s *Server) PushHub() *proactive.Hub               { return s.Mail.PushHub }
func (s *Server) PushNotifier() *push.Notifier          { return s.pushNotifier }
func (s *Server) Version() string                       { return s.version }
func (s *Server) StartedAt() time.Time                  { return s.startedAt }

// --- RPC / provider ---

func (s *Server) Dispatcher() *rpc.Dispatcher                        { return s.dispatcher }
func (s *Server) GatewaySubs() *events.GatewayEventSubscriptions     { return s.gatewaySubs }
func (s *Server) AuthManager() *provider.AuthManager                 { return s.authManager }
func (s *Server) ProviderRuntime() *provider.ProviderRuntimeResolver { return s.providerRuntime }

// --- Workflow / infra singletons ---

func (s *Server) Approvals() *approval.Store              { return s.approvals }
func (s *Server) JobTracker() *agent.JobTracker           { return s.jobTracker }
func (s *Server) UsageTracker() *usage.Tracker            { return s.usageTracker }
func (s *Server) MarketCache() *market.Cache              { return s.marketCache }
func (s *Server) MaintRunner() *maintenance.Runner        { return s.maintRunner }
func (s *Server) PromptOverride(id string) (string, bool) { return s.promptOverride(id) }
func (s *Server) PersonaOverride() string                 { return s.personaOverride() }
func (s *Server) MailAnalysisPrompt() string              { return s.mailAnalysisPrompt() }

// --- Memory (owned by servermail) ---
//
// Every Mail/Chat/Auto accessor nil-guards the manager so hand-built partial
// Server stubs in unit tests (and any pre-New() call) degrade to a zero value
// instead of panicking. Production always constructs all three in New().

func (s *Server) WikiStore() *wiki.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.WikiStore
}
func (s *Server) NotebookStore() *notebook.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.NotebookStore
}
func (s *Server) ContactsStore() *contacts.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.ContactsStore
}
func (s *Server) MailStore() *mailstore.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.MailStore
}
func (s *Server) WorkFeedStore() *workfeed.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.WorkFeedStore
}
func (s *Server) NativeSyncStore() *nativesync.Store {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.NativeSyncStore
}
func (s *Server) CPProjects() *mailflow.CounterpartyProjectsCache {
	if s.Mail == nil {
		return nil
	}
	return &s.Mail.CPProjects
}
func (s *Server) NativeWorkFeedAppender() tooldeps.WorkFeedRW {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.NativeWorkFeedStore()
}
func (s *Server) ProjectCandidatesFn() func() []mailanalysis.ProjectCandidate {
	if s.Mail == nil {
		return nil
	}
	return s.Mail.ProjectCandidatesFn()
}
func (s *Server) MailAnalysisModels() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string) {
	if s.Chat == nil {
		return nil, "", nil, ""
	}
	return s.Chat.MailAnalysisModels()
}

// --- Chat (owned by serverchat) ---

func (s *Server) ChatHandler() *chat.Handler         { return s.Chat.ChatHandler }
func (s *Server) ToolDeps() *chat.CoreToolDeps       { return s.Chat.ToolDeps }
func (s *Server) ModelRegistry() *modelrole.Registry { return s.Chat.ModelRegistry }
func (s *Server) ProactiveRelay() proactive.Relay    { return s.Chat.ProactiveRelay }
func (s *Server) Sessions() *session.Manager         { return s.Chat.Sessions }
func (s *Server) ResumableSessionForMarker(sessionKey string) (string, bool) {
	target, ok := serverchat.ResumableSessionForMarker(sessionKey)
	return target.Channel, ok
}
func (s *Server) CronService() *cron.Service { return s.Chat.CronService }
func (s *Server) ExternalMCPClient(name string) *mcpclient.Client {
	return s.Chat.ExternalMCPClient(name)
}
func (s *Server) FileSemindex() *filesemindex.Service       { return s.Chat.FileSemindex }
func (s *Server) PolarisStore() *polaris.Store              { return s.Chat.PolarisStore }
func (s *Server) SetGmailPollSvc(svc *mailanalysis.Service) { s.Auto.GmailPollSvc = svc }

// --- Auto / Genesis (owned by serverauto) ---

func (s *Server) AutonomousSvc() *autonomous.Service               { return s.Auto.AutonomousSvc }
func (s *Server) GenesisTracker() *skilllifecycle.Tracker          { return s.Auto.GenesisTracker }
func (s *Server) GenesisEvolver() *skilllifecycle.Evolver          { return s.Auto.GenesisEvolver }
func (s *Server) GenesisMeta() *skilllifecycle.MetaArtifacts       { return s.Auto.GenesisMeta }
func (s *Server) SkillCatalog() *skilllifecycle.Catalog            { return s.Auto.SkillCatalog }
func (s *Server) GenesisTranscripts() chat.TranscriptStore         { return s.Auto.GenesisTranscripts }
func (s *Server) SetGenesisTranscripts(store chat.TranscriptStore) { s.Auto.GenesisTranscripts = store }
func (s *Server) AgentLogWriter() *agentlog.Writer                 { return s.Auto.AgentLogWriter }
func (s *Server) SetAgentLogWriter(w *agentlog.Writer)             { s.Auto.AgentLogWriter = w }
func (s *Server) RoleHealth() *rolehealth.Watch                    { return s.Auto.RoleHealth }
func (s *Server) ModelMaintenance() *modelmaintenance.Suite        { return s.Auto.ModelMaintenance }
func (s *Server) WikiDreamer() *wiki.WikiDreamer                   { return s.Auto.WikiDreamer }
func (s *Server) SetWikiDreamer(d *wiki.WikiDreamer)               { s.Auto.WikiDreamer = d }

// --- Feed cards implemented by servermail, called from serverauto ---

func (s *Server) PostLowConfidenceEvolveCard(result genesis.EvolveResult) {
	s.Mail.PostLowConfidenceEvolveCard(result)
}
func (s *Server) PostMetaProposalCard(artifact, epoch, reason, path string, adopted bool) {
	s.Mail.PostMetaProposalCard(artifact, epoch, reason, path, adopted)
}
func (s *Server) PostMetaRevertedCard(artifact, reason string) {
	s.Mail.PostMetaRevertedCard(artifact, reason)
}
func (s *Server) PostDriftFreezeCard(frozen bool, reasons []string) {
	s.Mail.PostDriftFreezeCard(frozen, reasons)
}
func (s *Server) PostLadderReadyCard(title, detail string) error {
	return s.Mail.PostLadderReadyCard(title, detail)
}
func (s *Server) PostGraduationCard(key, title, evidence string) {
	s.Mail.PostGraduationCard(key, title, evidence)
}
func (s *Server) PostDreamWorkfeedCard(r *autonomous.DreamReport) {
	s.Mail.PostDreamWorkfeedCard(r)
}
