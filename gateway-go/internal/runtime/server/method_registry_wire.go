package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/servermail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire"
)

// wirePorts builds the explicit dependency bag for serverwire RPC registration.
// Capability init that mutates Server stays in initializeEarlyMethodCapabilities;
// this bag only carries values and callbacks the registration tables need.
// Composition-root fields (Mail/Chat/Auto) are read only when non-nil so
// partial Server stubs in unit tests do not panic.
func (s *Server) wirePorts() *serverwire.Ports {
	if s == nil {
		return &serverwire.Ports{}
	}
	ports := &serverwire.Ports{
		Logger:     s.logger,
		Dispatcher: s.dispatcher,
		DenebDir:   s.denebDir,

		Providers:   s.providers,
		AuthManager: s.authManager,
		PromptStore: s.promptStore,

		Caps: serverwire.CapabilityFlags{
			PushTokenStore: s.pushTokenStore,
			PushNotifier:   s.pushNotifier,
		},
	}
	if s.WorkflowSubsystem != nil {
		ports.UsageTracker = s.usageTracker
	}
	if s.InfraSubsystem != nil {
		ports.MaintRunner = s.maintRunner
	}
	if s.Mail != nil {
		ports.MailStore = s.Mail.MailStore
		ports.WikiStore = s.Mail.WikiStore
		ports.NotebookStore = s.Mail.NotebookStore
		ports.ContactsStore = s.Mail.ContactsStore
		ports.Caps.PushHub = s.Mail.PushHub
		ports.Caps.WorkFeedStore = s.Mail.WorkFeedStore
		ports.Caps.NativeSyncStore = s.Mail.NativeSyncStore
		ports.WorkFeed = serverwire.WorkFeedPorts{
			Store:          s.Mail.NativeWorkFeedStore(),
			OnAnswer:       s.Mail.RecordDealQuestionAnswer,
			OnMetaProposal: s.Mail.HandleMetaProposalAction,
		}
		ports.Mail = serverwire.MailAnalysisPorts{
			ClientFactory: s.Mail.MiniappMailClientFactory,
			SenderFacts:   s.Mail.WikiSenderFacts,
			Prompt:        s.mailAnalysisPrompt,
			ProjectsFn:    s.Mail.ProjectCandidatesFn,
			Ask:           s.Mail.MakeMailQAAsk,
			CpProjects:    &s.Mail.CPProjects,
		}
		ports.Phone = serverwire.PhonePorts{
			Ledger:          s.Mail.PhoneEventLedgerInstance,
			OnLocationPlace: s.Mail.SiteVisitOnLocation,
			ShutdownCtx:     s.ShutdownCtx,
			ResolveAction: func(res phoneevents.ActionResult) bool {
				return s.Mail.ResolvePhoneAction(servermail.PhoneActionResult{ID: res.ID, OK: res.OK, Error: res.Error})
			},
		}
		ports.MakeWikiMergeStarter = s.Mail.MakeWikiMergeStarter
		ports.OrgDeps = s.Mail.OrgDeps()
	}
	if s.Chat != nil {
		ports.ToolDeps = s.Chat.ToolDeps
		ports.ModelRegistry = s.Chat.ModelRegistry
		ports.ChatHandler = s.Chat.ChatHandler
		ports.FileSemindex = s.Chat.FileSemindex
		ports.ProactiveRelay = s.Chat.ProactiveRelay
		ports.CronService = s.Chat.CronService
		ports.Sessions = s.Chat.Sessions
		ports.ACPDeps = s.Chat.ACPDeps
		ports.Mail.Models = s.Chat.MailAnalysisModels
	}
	if s.Auto != nil {
		ports.RefreshCodingModelConsumers = s.Auto.RefreshCodingModelConsumers
		ports.Genesis = serverwire.GenesisPorts{
			Svc:         s.Auto.GenesisSvc,
			Evolver:     s.Auto.GenesisEvolver,
			Tracker:     s.Auto.GenesisTracker,
			Transcripts: s.Auto.GenesisTranscripts,
		}
		ports.ModelMaintenance = s.Auto.ModelMaintenance
		ports.RoleHealth = s.Auto.RoleHealth
	}
	return ports
}
