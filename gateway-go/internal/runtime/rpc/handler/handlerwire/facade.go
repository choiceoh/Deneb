// facade.go re-exports handler leaf packages under prefixed names. Straight
// type/var aliases only — no adapter logic.
package handlerwire

import (
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	minimodule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/module"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlerobservatory "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observatory"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
)

// --- handlerminiapp/module re-exports (import cycle + Methods name clash
// block putting these in the handlerminiapp package itself) ---

type (
	MiniContactsDeps              = minimodule.ContactsDeps
	MiniDashboardDeps             = minimodule.DashboardDeps
	MiniDashboardWorkFeedSource   = minimodule.DashboardWorkFeedSource
	MiniDependencies              = minimodule.Dependencies
	MiniMarketDeps                = minimodule.MarketDeps
	MiniRSIStatusDeps             = minimodule.RSIStatusDeps
	MiniSelfImprovementCodingDeps = minimodule.SelfImprovementCodingDeps
	MiniSessionsDeps              = minimodule.SessionsDeps
	MiniSkillsDeps                = minimodule.SkillsDeps
	MiniSyncDeps                  = minimodule.SyncDeps
	MiniTranscriptLoader          = minimodule.TranscriptLoader
)

var (
	MiniModuleMethods                = minimodule.Methods
	MiniOrgDashboardDeps             = minimodule.OrgDashboardDeps
	MiniRSIStatusMethods             = minimodule.RSIStatusMethods
	MiniSelfImprovementCodingMethods = minimodule.SelfImprovementCodingMethods
	MiniSkillsMethods                = minimodule.SkillsMethods
)

// --- agent re-exports ---

type AgentExtendedDeps = handleragent.ExtendedDeps

var AgentExtendedMethods = handleragent.ExtendedMethods

// --- checkpoint re-exports ---

type CheckpointDeps = handlercheckpoint.Deps

var CheckpointMethods = handlercheckpoint.Methods

// --- events re-exports ---

type EventsDeps = handlerevents.EventsDeps

var (
	BroadcastMethods = handlerevents.BroadcastMethods
	EventsMethods    = handlerevents.EventsMethods
)

// --- insights re-exports ---

type InsightsDeps = handlerinsights.Deps

var InsightsMethods = handlerinsights.Methods

// --- observatory re-exports ---

type ObservatoryDeps = handlerobservatory.Deps

var (
	ObservatoryMethods        = handlerobservatory.Methods
	ObservatoryMiniappMethods = handlerobservatory.MiniappMethods
)

// --- observe re-exports ---

type ObserveDeps = handlerobserve.Deps

var (
	ObserveMethods        = handlerobserve.Methods
	ObserveMiniappMethods = handlerobserve.MiniappMethods
)

// --- process re-exports ---

type (
	ProcessACPDeps          = handlerprocess.ACPDeps
	ProcessCronAdvancedDeps = handlerprocess.CronAdvancedDeps
	ProcessCronServiceDeps  = handlerprocess.CronServiceDeps
)

var (
	ProcessACPMethods          = handlerprocess.ACPMethods
	ProcessCronAdvancedMethods = handlerprocess.CronAdvancedMethods
	ProcessCronServiceMethods  = handlerprocess.CronServiceMethods
)

// --- provider re-exports ---

type (
	ProviderDeps       = handlerprovider.Deps
	ProviderModelsDeps = handlerprovider.ModelsDeps
)

var (
	ProviderMethods       = handlerprovider.Methods
	ProviderModelsMethods = handlerprovider.ModelsMethods
)

// --- session re-exports ---

type (
	SessionDeps              = handlersession.Deps
	SessionExecDeps          = handlersession.ExecDeps
	SessionTranscriptDeleter = handlersession.TranscriptDeleter
)

var (
	SessionCRUDMethods = handlersession.CRUDMethods
	SessionExecMethods = handlersession.ExecMethods
	SessionMethods     = handlersession.Methods
)

// --- skill re-exports ---

type (
	SkillDeps        = handlerskill.Deps
	SkillGenesisDeps = handlerskill.GenesisDeps
	SkillToolDeps    = handlerskill.ToolDeps
)

var (
	SkillGenesisMethods = handlerskill.GenesisMethods
	SkillMethods        = handlerskill.Methods
	SkillToolMethods    = handlerskill.ToolMethods
)

// --- system re-exports ---

type (
	SystemConfigAdvancedDeps = handlersystem.ConfigAdvancedDeps
	SystemConfigReloadDeps   = handlersystem.ConfigReloadDeps
	SystemHealthDeps         = handlersystem.HealthDeps
	SystemLogsDeps           = handlersystem.LogsDeps
	SystemMaintenanceDeps    = handlersystem.MaintenanceDeps
	SystemMonitoringDeps     = handlersystem.MonitoringDeps
	SystemUpdateDeps         = handlersystem.UpdateDeps
	SystemUsageDeps          = handlersystem.UsageDeps
)

var (
	SystemConfigAdvancedMethods = handlersystem.ConfigAdvancedMethods
	SystemConfigReloadMethods   = handlersystem.ConfigReloadMethods
	SystemHealthMethods         = handlersystem.HealthMethods
	SystemIdentityMethods       = handlersystem.IdentityMethods
	SystemLogsMethods           = handlersystem.LogsMethods
	SystemMaintenanceMethods    = handlersystem.MaintenanceMethods
	SystemMonitoringMethods     = handlersystem.MonitoringMethods
	SystemUpdateMethods         = handlersystem.UpdateMethods
	SystemUsageMethods          = handlersystem.UsageMethods
)

// --- mail re-exports (handlermail facade + gmail_context) ---

type (
	MailAnalyzePipeline   = handlermail.AnalyzePipeline
	MailCachedAnalysis    = handlermail.CachedAnalysis
	MailGmailAnalyzeDeps  = handlermail.GmailAnalyzeDeps
	MailGmailClient       = handlermail.GmailClient
	MailGmailContextDeps  = handlermail.GmailContextDeps
	MailGmailDeps         = handlermail.GmailDeps
	MailQATurn            = handlermail.QATurn
	MailStoreReader       = handlermail.MailStoreReader
	MailWikiAnalysisInput = handlermail.WikiAnalysisInput
)

var (
	MailErrAnalyzeNoLLM          = handlermail.ErrAnalyzeNoLLM
	MailGmailAnalyzeMethods      = handlermail.GmailAnalyzeMethods
	MailGmailContextMethods      = handlermail.GmailContextMethods
	MailGmailMethods             = handlermail.GmailMethods
	MailNewAnalysisStore         = handlermail.NewAnalysisStore
	MailPipelineFromMailAnalysis = handlermail.PipelineFromMailAnalysis
)

// --- chat re-exports ---

type (
	ChatBtwDeps = handlerchat.BtwDeps
	ChatDeps    = handlerchat.Deps
)

var (
	ChatBtwMethods     = handlerchat.BtwMethods
	ChatMethods        = handlerchat.Methods
	ChatMiniappMethods = handlerchat.MiniappMethods
)

// --- wiki re-exports ---

type WikiDeps = handlerwiki.Deps

var WikiMethods = handlerwiki.Methods
