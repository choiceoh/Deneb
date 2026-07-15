// Package domainbind re-exports domain packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package domainbind

import (
	approval "github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	autonomous "github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	contacts "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	filestore "github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	goals "github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	mailpriority "github.com/choiceoh/deneb/gateway-go/internal/domain/mailpriority"
	maintenance "github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	market "github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	monitoring "github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	nativesync "github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	notebook "github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	org "github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	prompts "github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	push "github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	domainsession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	skills "github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	wikiport "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	workfeed "github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// --- domain/approval ---

type (
	CreateRequestParams = approval.CreateRequestParams
	ApprovalStore       = approval.Store
)

var (
	DecisionAllowAlways = approval.DecisionAllowAlways
	DecisionAllowOnce   = approval.DecisionAllowOnce
	NewApprovalStore    = approval.NewStore
)

// --- domain/autonomous ---

type (
	CycleEvent  = autonomous.CycleEvent
	DreamReport = autonomous.DreamReport
	Service     = autonomous.Service
)

var (
	NewService               = autonomous.NewService
	SignalConfigForThreshold = autonomous.SignalConfigForThreshold
)

// --- domain/contacts ---

type (
	Contact       = contacts.Contact
	ContactsStore = contacts.Store
)

var (
	NewContactsStore    = contacts.NewStore
	NormalizePersonName = contacts.NormalizePersonName
)

// --- domain/filestore ---

type (
	ScoredEntry = filestore.ScoredEntry
	FileStore   = filestore.Store
)

var DefaultLocalStore = filestore.DefaultLocalStore

// --- domain/goals ---

var (
	NewGoalsStore = goals.NewStore
	SetDefault    = goals.SetDefault
)

// --- domain/mailpriority ---

type (
	Scorer = mailpriority.Scorer
)

var NewMailPriority = mailpriority.New

// --- domain/maintenance ---

type (
	Runner = maintenance.Runner
)

var NewRunner = maintenance.NewRunner

// --- domain/market ---

type (
	Cache = market.Cache
	Quote = market.Quote
)

var NewCache = market.NewCache

// --- domain/monitoring ---

type (
	ActivityTracker = monitoring.ActivityTracker
)

var NewActivityTracker = monitoring.NewActivityTracker

// --- domain/nativesync ---

type (
	AppendInput     = nativesync.AppendInput
	NativeSyncStore = nativesync.Store
)

var (
	CalendarChanged    = nativesync.CalendarChanged
	NewNativeSyncStore = nativesync.NewStore
	WorkFeedActionRun  = nativesync.WorkFeedActionRun
	WorkFeedCreated    = nativesync.WorkFeedCreated
	WorkFeedUpdated    = nativesync.WorkFeedUpdated
)

// --- domain/notebook ---

type (
	Source        = notebook.Source
	NotebookStore = notebook.Store
)

var (
	KindNote         = notebook.KindNote
	NewNotebookStore = notebook.NewStore
)

// --- domain/org ---

type (
	OrgTree = org.OrgTree
)

var (
	Load        = org.Load
	LoadLanes   = org.LoadLanes
	LoadRules   = org.LoadRules
	ResolvePath = org.ResolvePath
)

// --- domain/prompts ---

type (
	PromptsStore = prompts.Store
	Template     = prompts.Template
)

var NewPromptsStore = prompts.NewStore

// --- domain/push ---

type (
	Notifier     = push.Notifier
	NotifierDeps = push.NotifierDeps
	PushStore    = push.Store
)

var (
	PushConfigFromEnv = push.ConfigFromEnv
	NewFCMSender      = push.NewFCMSender
	NewNotifier       = push.NewNotifier
	NewPushStore      = push.NewStore
)

// --- domain/session ---

type (
	Event          = domainsession.Event
	EventBus       = domainsession.EventBus
	LifecycleEvent = domainsession.LifecycleEvent
	Manager        = domainsession.Manager
	RunMarker      = domainsession.RunMarker
	Session        = domainsession.Session
)

var (
	EventCreated                = domainsession.EventCreated
	EventDeleted                = domainsession.EventDeleted
	EventStatusChanged          = domainsession.EventStatusChanged
	IsTerminal                  = domainsession.IsTerminal
	KindCron                    = domainsession.KindCron
	KindDirect                  = domainsession.KindDirect
	NativeWorkSessionKey        = domainsession.NativeWorkSessionKey
	NewManager                  = domainsession.NewManager
	PhaseEnd                    = domainsession.PhaseEnd
	PhaseStart                  = domainsession.PhaseStart
	RestorableTranscriptChannel = domainsession.RestorableTranscriptChannel
	StatusDone                  = domainsession.StatusDone
	StatusFailed                = domainsession.StatusFailed
	StatusKilled                = domainsession.StatusKilled
	StatusRunning               = domainsession.StatusRunning
	StatusTimeout               = domainsession.StatusTimeout
)

// --- domain/skills ---

type (
	Registry   = skills.Registry
	SkillEntry = skills.SkillEntry
)

var (
	DefaultManagedSkillsDir  = skills.DefaultManagedSkillsDir
	DefaultPersonalSkillsDir = skills.DefaultPersonalSkillsDir
	NewRegistry              = skills.NewRegistry
)

// --- domain/wikiport ---

type (
	ContactEnrichResult = wikiport.ContactEnrichResult
	DealPageInput       = wikiport.DealPageInput
	DealTerms           = wikiport.DealTerms
	Frontmatter         = wikiport.Frontmatter
	MergeOptions        = wikiport.MergeOptions
	Page                = wikiport.Page
	PersonSeed          = wikiport.PersonSeed
	QuotedTerm          = wikiport.QuotedTerm
	WikiStore           = wikiport.Store
	WikiDreamer         = wikiport.WikiDreamer
)

var (
	WikiConfigFromEnv    = wikiport.ConfigFromEnv
	IsProjectRepPage     = wikiport.IsProjectRepPage
	MailAnalysisPagePath = wikiport.MailAnalysisPagePath
	NewWikiStore         = wikiport.NewStore
	NewWikiDreamer       = wikiport.NewWikiDreamer
	ProjectNameOf        = wikiport.ProjectNameOf
)

// --- domain/workfeed ---

type (
	Action        = workfeed.Action
	ActionEffect  = workfeed.ActionEffect
	ActionResult  = workfeed.ActionResult
	Item          = workfeed.Item
	WorkFeedStore = workfeed.Store
)

var (
	ActionAck           = workfeed.ActionAck
	NewWorkFeedStore    = workfeed.NewStore
	SourceDream         = workfeed.SourceDream
	SourceMeetingReport = workfeed.SourceMeetingReport
	StatusAcked         = workfeed.StatusAcked
	StatusUnread        = workfeed.StatusUnread
)
