// Package svcbind re-exports runtime service packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package svcbind

import (
	configresolve "github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	cronrunner "github.com/choiceoh/deneb/gateway-go/internal/runtime/cronrunner"
	curriculumenv "github.com/choiceoh/deneb/gateway-go/internal/runtime/curriculumenv"
	events "github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	filesemindex "github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	gatewayhttp "github.com/choiceoh/deneb/gateway-go/internal/runtime/gatewayhttp"
	goalloop "github.com/choiceoh/deneb/gateway-go/internal/runtime/goalloop"
	runtimehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/health"
	heartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	insights "github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	mailflow "github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	meeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	modelmaintenance "github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	modelpicker "github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpicker"
	nativeauth "github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
	phoneevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	proactive "github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	rolehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	sessionstore "github.com/choiceoh/deneb/gateway-go/internal/runtime/sessionstore"
	skilllifecycle "github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
	wikiwork "github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// --- runtime/configresolve ---

var (
	CodingModel                = configresolve.CodingModel
	CurrentTopicKey            = configresolve.CurrentTopicKey
	DefaultModel               = configresolve.DefaultModel
	DenebDir                   = configresolve.DenebDir
	FallbackModel              = configresolve.FallbackModel
	LightweightModel           = configresolve.LightweightModel
	LoadProviderConfigs        = configresolve.LoadProviderConfigs
	LocalVLLMModel             = configresolve.LocalVLLMModel
	ProactiveEscalateThreshold = configresolve.ProactiveEscalateThreshold
	ProviderCatalog            = configresolve.ProviderCatalog
	SessionThinkingDefaults    = configresolve.SessionThinkingDefaults
	SubagentDefaultModel       = configresolve.SubagentDefaultModel
	TinyModel                  = configresolve.TinyModel
	TopicsDir                  = configresolve.TopicsDir
	VisionModel                = configresolve.VisionModel
	WorkspaceDir               = configresolve.WorkspaceDir
)

// --- runtime/cronrunner ---

type (
	CronRunnerConfig = cronrunner.Config
)

var (
	NewCronRunner     = cronrunner.New
	NewSubagentPoller = cronrunner.NewSubagentPoller
)

// --- runtime/curriculumenv ---

type (
	Sources = curriculumenv.Sources
)

var Digest = curriculumenv.Digest

// --- runtime/events ---

type (
	AgentEvent                = events.AgentEvent
	Broadcaster               = events.Broadcaster
	EventPayload              = events.EventPayload
	GatewayEventSubscriptions = events.GatewayEventSubscriptions
	GatewaySubscriptionParams = events.GatewaySubscriptionParams
	Publisher                 = events.Publisher
	SessionSnapshot           = events.SessionSnapshot
	SessionSnapshotProvider   = events.SessionSnapshotProvider
	TranscriptUpdate          = events.TranscriptUpdate
)

var (
	NewBroadcaster               = events.NewBroadcaster
	NewGatewayEventSubscriptions = events.NewGatewayEventSubscriptions
	NewPublisher                 = events.NewPublisher
	PayloadFromRaw               = events.PayloadFromRaw
)

// --- runtime/filesemindex ---

type (
	FileSemIndexService = filesemindex.Service
)

var NewFileSemIndex = filesemindex.New

// --- runtime/gatewayhttp ---

type (
	GatewayHTTPConfig    = gatewayhttp.Config
	FleetAlertConfig     = gatewayhttp.FleetAlertConfig
	MailAttachmentClient = gatewayhttp.MailAttachmentClient
)

var (
	RegisterFleetAlertRoute = gatewayhttp.RegisterFleetAlertRoute
	RegisterRoutes          = gatewayhttp.RegisterRoutes
	WithCORS                = gatewayhttp.WithCORS
)

// --- runtime/goalloop ---

var NewGoalLoopTask = goalloop.NewTask

// --- runtime/health ---

type (
	Probes        = runtimehealth.Probes
	PropusSection = runtimehealth.PropusSection
)

var Propus = runtimehealth.Propus

// --- runtime/heartbeat ---

type (
	ShadowCompleteFunc = heartbeat.ShadowCompleteFunc
	ShadowReplayReport = heartbeat.ShadowReplayReport
	TaskConfig         = heartbeat.TaskConfig
)

var (
	CalendarSignalCollector     = heartbeat.CalendarSignalCollector
	CombineCollectors           = heartbeat.CombineCollectors
	DealDeadlineSignalCollector = heartbeat.DealDeadlineSignalCollector
	FixturePath                 = heartbeat.FixturePath
	LastSelfCodingNudgeAtMillis = heartbeat.LastSelfCodingNudgeAtMillis
	NewBootTask                 = heartbeat.NewBootTask
	NewHeartbeatTask            = heartbeat.NewTask
	RunShadowReplay             = heartbeat.RunShadowReplay
	TodoDeadlineCollector       = heartbeat.TodoDeadlineCollector
)

// --- runtime/insights ---

type (
	Engine   = insights.Engine
	ToolStat = insights.ToolStat
)

var NewInsights = insights.New

// --- runtime/mailflow ---

type (
	CounterpartyProjectsCache = mailflow.CounterpartyProjectsCache
)

var (
	CalendarProposalsFromMail = mailflow.CalendarProposalsFromMail
	DocumentAttachmentNames   = mailflow.DocumentAttachmentNames
	NewCounterpartyLookup     = mailflow.NewCounterpartyLookup
)

// --- runtime/meeting ---

type (
	CalendarBriefingService = meeting.CalendarBriefingService
	HarvestService          = meeting.HarvestService
	PlaudService            = meeting.PlaudService
)

var (
	HarvestStateFile           = meeting.HarvestStateFile
	KnownNames                 = meeting.KnownNames
	LooseUniqueNameMatch       = meeting.LooseUniqueNameMatch
	MeetingMatchText           = meeting.MeetingMatchText
	NewCalendarBriefingService = meeting.NewCalendarBriefingService
	NewHarvestService          = meeting.NewHarvestService
	NewPlaudService            = meeting.NewPlaudService
	PlaudDisableEnv            = meeting.PlaudDisableEnv
	PlaudStateFile             = meeting.PlaudStateFile
	ResolveCalendarClient      = meeting.ResolveCalendarClient
)

// --- runtime/modelmaintenance ---

type (
	ModelMaintenanceDeps = modelmaintenance.Deps
	Suite                = modelmaintenance.Suite
)

var NewModelMaintenance = modelmaintenance.New

// --- runtime/modelpicker ---

type (
	ControllerConfig = modelpicker.ControllerConfig
)

var NewController = modelpicker.NewController

// --- runtime/nativeauth ---

var Authenticate = nativeauth.Authenticate

// --- runtime/notify ---

type (
	NotifyService    = runtimenotify.Service
	SwappableHandler = runtimenotify.SwappableHandler
)

var (
	NewService          = runtimenotify.NewService
	NewSwappableHandler = runtimenotify.NewSwappableHandler
)

// --- runtime/phoneevents ---

type (
	ActionResult      = phoneevents.ActionResult
	PhoneEventsConfig = phoneevents.Config
	Handler           = phoneevents.Handler
	Ledger            = phoneevents.Ledger
)

var (
	LedgerDirname  = phoneevents.LedgerDirname
	NewPhoneEvents = phoneevents.New
	NewLedger      = phoneevents.NewLedger
)

// --- runtime/proactive ---

type (
	AlertGate     = proactive.AlertGate
	ProactiveDeps = proactive.Deps
	Event         = proactive.Event
	Hub           = proactive.Hub
	Options       = proactive.Options
	Relay         = proactive.Relay
)

var (
	CardTitleSummary        = proactive.CardTitleSummary
	DreamWorkSessionKey     = proactive.DreamWorkSessionKey
	KindDesktop             = proactive.KindDesktop
	KindMobile              = proactive.KindMobile
	NativeWorkSessionKey    = proactive.NativeWorkSessionKey
	NativeWorkSessionTarget = proactive.NativeWorkSessionTarget
	NewAlertGate            = proactive.NewAlertGate
	NewHub                  = proactive.NewHub
	NewRelay                = proactive.NewRelay
	PublishWithFallback     = proactive.PublishWithFallback
	PushKindFleet           = proactive.PushKindFleet
	PushKindPhoneAction     = proactive.PushKindPhoneAction
)

// --- runtime/rolehealth ---

type (
	Watch = rolehealth.Watch
)

var NewRoleHealth = rolehealth.New

// --- runtime/sessionstore ---

type (
	RunMarkerStore = sessionstore.RunMarkerStore
)

var NewRunMarkerStore = sessionstore.NewRunMarkerStore

// --- runtime/skilllifecycle ---

type (
	BackendConfig  = skilllifecycle.BackendConfig
	Catalog        = skilllifecycle.Catalog
	CoreBuildInput = skilllifecycle.CoreBuildInput
	Evolver        = skilllifecycle.Evolver
	GenesisService = skilllifecycle.GenesisService
	MetaArtifacts  = skilllifecycle.MetaArtifacts
	Nudger         = skilllifecycle.Nudger
	Tracker        = skilllifecycle.Tracker
)

var (
	BuildCore                              = skilllifecycle.BuildCore
	BuildSessionContext                    = skilllifecycle.BuildSessionContext
	DefaultMetaArtifacts                   = skilllifecycle.DefaultMetaArtifacts
	DefaultNudgeInterval                   = skilllifecycle.DefaultNudgeInterval
	NewBackend                             = skilllifecycle.NewBackend
	NewChatUsageRecorder                   = skilllifecycle.NewChatUsageRecorder
	NewNudgerFromEnvWithTrackerAndReviewer = skilllifecycle.NewNudgerFromEnvWithTrackerAndReviewer
	NewReviewFork                          = skilllifecycle.NewReviewFork
	NewSkillNudger                         = skilllifecycle.NewSkillNudger
	NewValidationBackfillTask              = skilllifecycle.NewValidationBackfillTask
)

// --- runtime/wikiwork ---

type (
	ScoutTask         = wikiwork.ScoutTask
	SiteVisitRecorder = wikiwork.SiteVisitRecorder
)

var (
	DriveFolderEnv                = wikiwork.DriveFolderEnv
	NewNotiDigestTask             = wikiwork.NewNotiDigestTask
	NewResearchTask               = wikiwork.NewResearchTask
	NewReviewTask                 = wikiwork.NewReviewTask
	NewScoutTask                  = wikiwork.NewScoutTask
	NewSiteVisitRecorder          = wikiwork.NewSiteVisitRecorder
	NewSupernoteDigestTask        = wikiwork.NewSupernoteDigestTask
	NotiDigestInterval            = wikiwork.NotiDigestInterval
	NotiDigestStateFile           = wikiwork.NotiDigestStateFile
	RecordMeetingAttendanceByPath = wikiwork.RecordMeetingAttendanceByPath
	ResearchInterval              = wikiwork.ResearchInterval
	ResearchStateFile             = wikiwork.ResearchStateFile
	ReviewInterval                = wikiwork.ReviewInterval
	ReviewStateFile               = wikiwork.ReviewStateFile
	ScoutInterval                 = wikiwork.ScoutInterval
	ScoutStateFile                = wikiwork.ScoutStateFile
	SiteVisitStateFile            = wikiwork.SiteVisitStateFile
	SupernoteInterval             = wikiwork.SupernoteInterval
	SupernoteStateFile            = wikiwork.SupernoteStateFile
)

// PayloadOf forwards the generic helper (cannot var-alias generics).
func PayloadOf[T any](v T) (EventPayload, error) {
	return events.PayloadOf(v)
}
