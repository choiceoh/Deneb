// Package pipebind re-exports pipeline packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package pipebind

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	chattranscript "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// --- pipeline/autoreply/acp ---

type (
	ResultInjectionDeps  = acp.ResultInjectionDeps
	SubagentInfraDeps    = acp.SubagentInfraDeps
	TranscriptAppendFunc = acp.TranscriptAppendFunc
)

var (
	DefaultBindingStorePath      = acp.DefaultBindingStorePath
	DefaultRegistryStorePath     = acp.DefaultRegistryStorePath
	NewACPProjector              = acp.NewACPProjector
	NewACPRegistry               = acp.NewACPRegistry
	NewACPTranslator             = acp.NewACPTranslator
	NewBindingStore              = acp.NewBindingStore
	NewRegistryStore             = acp.NewRegistryStore
	NewSessionBindingService     = acp.NewSessionBindingService
	StartACPLifecycleSync        = acp.StartACPLifecycleSync
	StartSubagentResultInjection = acp.StartSubagentResultInjection
)

// --- pipeline/autoreply/session ---

type (
	AbortMemory    = arSession.AbortMemory
	HistoryTracker = arSession.HistoryTracker
	SessionUsage   = arSession.SessionUsage
)

var (
	NewAbortMemory    = arSession.NewAbortMemory
	NewHistoryTracker = arSession.NewHistoryTracker
)

// --- pipeline/chat ---

type (
	CalendarDeps       = chat.CalendarDeps
	CalendarGlanceFunc = chat.CalendarGlanceFunc
	ChronoDeps         = chat.ChronoDeps
	ContactsDeps       = chat.ContactsDeps
	CoreToolDeps       = chat.CoreToolDeps
	FleetDeps          = chat.FleetDeps
	Handler            = chat.Handler
	HandlerConfig      = chat.HandlerConfig
	LinkEnrichStart    = chat.LinkEnrichStart
	NotebookDeps       = chat.NotebookDeps
	ProcessDeps        = chat.ProcessDeps
	SessionDeps        = chat.SessionDeps
	StatusDeps         = chat.StatusDeps
	SyncOptions        = chat.SyncOptions
	ToolRegistry       = chat.ToolRegistry
	TopicResolver      = chat.TopicResolver
	TranscriptStore    = chat.TranscriptStore
	WikiDeps           = chat.WikiDeps
)

var (
	BundledSkillsDir         = chat.BundledSkillsDir
	ConfigurePromptSnapshots = chat.ConfigurePromptSnapshots
	DefaultHandlerConfig     = chat.DefaultHandlerConfig
	LoadPromptSnapshots      = chat.LoadPromptSnapshots
	NewGoalGlanceFunc        = chat.NewGoalGlanceFunc
	NewHandler               = chat.NewHandler
	NewTextChatMessage       = chat.NewTextChatMessage
	NewToolRegistry          = chat.NewToolRegistry
	RegisterCoreTools        = chat.RegisterCoreTools
	EligibleWorkspaceSkills  = chat.EligibleWorkspaceSkills
	InvalidateSkillsCache    = chat.InvalidateSkillsCache
)

// --- pipeline/chat/prompt ---

var (
	ClearSessionSnapshot  = prompt.ClearSessionSnapshot
	DefaultPersona        = prompt.DefaultPersona
	LoadTopicKnowledge    = prompt.LoadTopicKnowledge
	PromptIDSystemPersona = prompt.PromptIDSystemPersona
	Cache                 = prompt.Cache
)

// --- pipeline/chat/streaming ---

type (
	BroadcastRawFunc = streaming.BroadcastRawFunc
)

// --- pipeline/chat/tooldeps ---

type (
	AgentLogAggregate   = tooldeps.AgentLogAggregate
	AgentLogSessionStat = tooldeps.AgentLogSessionStat
	AgentLogStats       = tooldeps.AgentLogStats
	CalendarAttendee    = tooldeps.CalendarAttendee
	CalendarConference  = tooldeps.CalendarConference
	CalendarCreateInput = tooldeps.CalendarCreateInput
	CalendarEvent       = tooldeps.CalendarEvent
	CalendarReader      = tooldeps.CalendarReader
	Contact             = tooldeps.Contact
	ContactsBook        = tooldeps.ContactsBook
	FileHit             = tooldeps.FileHit
	LocalCalendar       = tooldeps.LocalCalendar
	MarketQuote         = tooldeps.MarketQuote
	ObserveToolFunc     = tooldeps.ObserveToolFunc
	WorkFeedAction      = tooldeps.WorkFeedAction
	WorkFeedItem        = tooldeps.WorkFeedItem
	WorkFeedRW          = tooldeps.WorkFeedRW
)

// --- pipeline/chat/toolport ---

type (
	ToolDef                 = toolport.ToolDef
	ToolportTranscriptStore = toolport.TranscriptStore
)

// --- pipeline/chat/transcript ---

var (
	NewCachedTranscriptStore = chattranscript.NewCachedTranscriptStore
	NewFileTranscriptStore   = chattranscript.NewFileTranscriptStore
)

// --- pipeline/chatport ---

type (
	ProviderConfig = chatport.ProviderConfig
)

// --- pipeline/pilot ---

var (
	CallLocalLLM  = pilot.CallLocalLLM
	CallRoleLLM   = pilot.CallRoleLLM
	LocalAIHub    = pilot.LocalAIHub
	SetLocalAIHub = pilot.SetLocalAIHub
)

// --- pipeline/polaris ---

type (
	Bridge      = polaris.Bridge
	Store       = polaris.Store
	SummaryNode = polaris.SummaryNode
)

var (
	NewBridge = polaris.NewBridge
	NewStore  = polaris.NewStore
)
