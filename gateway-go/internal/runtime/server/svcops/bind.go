// Package svcops re-exports runtime service packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package svcops

import (
	cronrunner "github.com/choiceoh/deneb/gateway-go/internal/runtime/cronrunner"
	curriculumenv "github.com/choiceoh/deneb/gateway-go/internal/runtime/curriculumenv"
	filesemindex "github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	goalloop "github.com/choiceoh/deneb/gateway-go/internal/runtime/goalloop"
	heartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	insights "github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	mailflow "github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	meeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	modelmaintenance "github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	proactive "github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	wikiwork "github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
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

// --- runtime/filesemindex ---

type (
	FileSemIndexService = filesemindex.Service
)

var NewFileSemIndex = filesemindex.New

// --- runtime/goalloop ---

var NewGoalLoopTask = goalloop.NewTask

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
