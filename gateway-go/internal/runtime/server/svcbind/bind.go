// Package svcbind re-exports runtime service packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package svcbind

import (
	configresolve "github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	events "github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	gatewayhttp "github.com/choiceoh/deneb/gateway-go/internal/runtime/gatewayhttp"
	runtimehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/health"
	modelpicker "github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpicker"
	nativeauth "github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
	phoneevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	rolehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	sessionstore "github.com/choiceoh/deneb/gateway-go/internal/runtime/sessionstore"
	skilllifecycle "github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
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

// --- runtime/health ---

type (
	Probes        = runtimehealth.Probes
	PropusSection = runtimehealth.PropusSection
)

var Propus = runtimehealth.Propus

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

// PayloadOf forwards the generic helper (cannot var-alias generics).
func PayloadOf[T any](v T) (EventPayload, error) {
	return events.PayloadOf(v)
}
