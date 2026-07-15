// Package platbind re-exports platform packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package platbind

import (
	calendar "github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	calprop "github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	platformcron "github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	gmail "github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	lmtpd "github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
	localcal "github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	localtodo "github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	mailanalysis "github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	mailarchive "github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	mailstore "github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
	mailwork "github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	mcpclient "github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
)

// --- platform/calendar ---

type (
	Attendee       = calendar.Attendee
	CalendarClient = calendar.Client
	Event          = calendar.Event
)

var DefaultCalendarClient = calendar.DefaultClient

// --- platform/calprop ---

var CalPropDefault = calprop.Default

// --- platform/cron ---

type (
	CronEvent        = platformcron.CronEvent
	PersistentRunLog = platformcron.PersistentRunLog
	CronService      = platformcron.Service
	ServiceConfig    = platformcron.ServiceConfig
	StoreJob         = platformcron.StoreJob
	StorePayload     = platformcron.StorePayload
	StoreSchedule    = platformcron.StoreSchedule
)

var (
	DefaultCronStorePath = platformcron.DefaultCronStorePath
	NewPersistentRunLog  = platformcron.NewPersistentRunLog
	NewCronService       = platformcron.NewService
)

// --- platform/gmail ---

type (
	MessageDetail = gmail.MessageDetail
)

var DefaultGmailClient = gmail.DefaultClient

// --- platform/lmtpd ---

type (
	Message   = lmtpd.Message
	Queue     = lmtpd.Queue
	QueueItem = lmtpd.QueueItem
	SeenStore = lmtpd.SeenStore
)

var (
	NewLmtpd     = lmtpd.New
	NewQueue     = lmtpd.NewQueue
	NewSeenStore = lmtpd.NewSeenStore
	ParseMessage = lmtpd.ParseMessage
)

// --- platform/localcal ---

type (
	CreateInput   = localcal.CreateInput
	LocalCalStore = localcal.Store
)

var LocalCalDefault = localcal.Default

// --- platform/localtodo ---

var LocalTodoDefault = localtodo.Default

// --- platform/mailanalysis ---

type (
	ActionItem          = mailanalysis.ActionItem
	AnalysisResult      = mailanalysis.AnalysisResult
	MailAnalysisConfig  = mailanalysis.Config
	DealFacts           = mailanalysis.DealFacts
	DealInfo            = mailanalysis.DealInfo
	Notifier            = mailanalysis.Notifier
	ProjectCandidate    = mailanalysis.ProjectCandidate
	QuotedFact          = mailanalysis.QuotedFact
	MailAnalysisService = mailanalysis.Service
	ThreadSource        = mailanalysis.ThreadSource
)

var (
	DefaultPrompt            = mailanalysis.DefaultPrompt
	ExtractForEval           = mailanalysis.ExtractForEval
	ExtractSenderFacts       = mailanalysis.ExtractSenderFacts
	NewMailAnalysisService   = mailanalysis.NewService
	OurMailDomains           = mailanalysis.OurMailDomains
	PromptIDAutoMailAnalysis = mailanalysis.PromptIDAutoMailAnalysis
)

// --- platform/mailarchive ---

type (
	MailArchiveConfig = mailarchive.Config
	ContextMessage    = mailarchive.ContextMessage
	ContextOptions    = mailarchive.ContextOptions
	FallbackClient    = mailarchive.FallbackClient
	Repository        = mailarchive.Repository
	RepositoryOptions = mailarchive.RepositoryOptions
)

var (
	ContextMessageFromDetail = mailarchive.ContextMessageFromDetail
	FetchAllContextMessages  = mailarchive.FetchAllContextMessages
	NewMailArchive           = mailarchive.New
	NewRepository            = mailarchive.NewRepository
	ParseMailboxList         = mailarchive.ParseMailboxList
)

// --- platform/mailstore ---

type (
	MailStore = mailstore.Store
)

var NewMailStore = mailstore.New

// --- platform/mailwork ---

type (
	AnalysisInput = mailwork.AnalysisInput
	MessageInput  = mailwork.MessageInput
)

var NewMailWork = mailwork.New

// --- platform/mcpclient ---

type (
	MCPClient = mcpclient.Client
)
