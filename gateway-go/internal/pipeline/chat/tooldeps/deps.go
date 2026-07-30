package tooldeps

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

// Type aliases so chat parent code can name wiki/notebook stores without
// importing those domain packages directly (fanout reduction).
type (
	WikiStore     = wiki.Store
	NotebookStore = notebook.Store
)

// TranscriptStore is the stable transcript contract owned by chatport.
type TranscriptStore = chatport.TranscriptStore

// PhoneActionFunc delivers a structured phone action to the native app for
// in-app Intent execution. The server backs it with the app's live push
// channel; tools consume only this stable dependency contract.
//
// Result contract: a nil error means the app confirmed launch. A returned
// error wrapping ErrPhoneActionUnconfirmed means the frame was delivered but no
// execution report arrived in time. Any other error is a real delivery or
// device-reported failure.
type PhoneActionFunc func(ctx context.Context, action string, args map[string]string) error

// ErrPhoneActionUnconfirmed marks the fail-open outcome of a phone action
// dispatch: the frame reached the push channel but the app did not report an
// execution result within the wait window.
var ErrPhoneActionUnconfirmed = errors.New("phone action execution not confirmed")

// MarketQuote is one instrument quote for the market tool.
type MarketQuote struct {
	Symbol    string
	Label     string
	Currency  string
	Price     float64
	PrevClose float64
	AsOf      int64
}

// FileHit is one semantic-search hit for the files tool.
type FileHit struct {
	Path    string
	Name    string
	Score   float64
	Snippet string
}

// WorkFeedAction is one chip/action on a work-feed card.
type WorkFeedAction struct {
	ID     string
	Kind   string
	Label  string
	Status string
	Prompt string
}

// WorkFeedItem is the work-feed card shape the workfeed tool mutates.
type WorkFeedItem struct {
	ID             string
	Source         string
	Title          string
	Summary        string
	Body           string
	SessionKey     string
	RefType        string
	RefID          string
	Metadata       map[string]string
	ClusterID      string
	RelatedIDs     []string
	Status         string
	Priority       int
	Question       bool
	Actions        []WorkFeedAction
	CreatedAtMs    int64
	UpdatedAtMs    int64
	SnoozedUntilMs int64
	ReadAtMs       int64
}

// Work-feed priority levels (mirrors domain/workfeed constants).
// Higher values surface first in the feed.
const (
	WorkFeedPriorityLow    = 1
	WorkFeedPriorityNormal = 2
	WorkFeedPriorityHigh   = 3
	WorkFeedPriorityUrgent = 4
)

// WorkFeedSourceDocAnalysis is the source tag for agent-published cards.
const WorkFeedSourceDocAnalysis = "doc_analysis"

// ObserveToolFunc is the observe tool executor injected by the composition root.
type ObserveToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// TextReranker is the narrow cross-encoder contract needed by tool discovery.
// Implementations are optional; callers must preserve their retrieval order on
// every error so the reranker never becomes load-bearing.
type TextReranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
}

// SpilloverStore spills large tool results to disk and loads them back by ID.
// Satisfied by *agent.SpilloverStore — no adapter needed at wire sites.
type SpilloverStore interface {
	Load(spillID, sessionKey string) (string, error)
	Store(sessionKey, toolName, content string) (string, error)
	CleanSession(sessionKey string)
}

// AgentLogAggregate is the cross-session roll-up sessions action=stats needs.
type AgentLogAggregate struct {
	Runs              int
	ProactiveRuns     int
	TotalInputTokens  int64
	TotalOutputTokens int64
	CacheReadTokens   int64
}

// AgentLogSessionStat is one per-session row for sessions action=stats.
type AgentLogSessionStat struct {
	Session      string
	Runs         int
	Errors       int
	InputTokens  int64
	OutputTokens int64
	ToolCalls    int
	LastTs       int64
}

// AgentLogStats is the narrow agent-log slice sessions action=stats needs.
type AgentLogStats interface {
	Aggregate(sinceMs int64) AgentLogAggregate
	AggregateBySession(sinceMs int64) []AgentLogSessionStat
}

// Contact is one address-book entry the contacts/wiki tools read.
type Contact struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
	Org    string   `json:"org,omitempty"`
}

// ContactsBook is the read-only address-book port for contacts + wiki enrichment.
type ContactsBook interface {
	Count() int
	LookupPhone(query string) []Contact
	Search(query string, limit int) []Contact
	All() []Contact
}

// CalendarEvent is one calendar event the schedule/codeaction tools read/write.
type CalendarEvent struct {
	ID          string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	HTMLLink    string
	Status      string
	Organizer   CalendarAttendee
	Attendees   []CalendarAttendee
	Conference  *CalendarConference

	Source      string
	SourceLabel string
	Kind        string
	Docs        []string
}

// CalendarAttendee is a calendar participant.
type CalendarAttendee struct {
	Email          string
	DisplayName    string
	ResponseStatus string
	Self           bool
	Organizer      bool
}

// CalendarConference describes an attached video conference.
type CalendarConference struct {
	Solution string
	URI      string
}

// CalendarCreateInput is the user-settable subset for local calendar writes.
type CalendarCreateInput struct {
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Source      string
	SourceLabel string
	Kind        string
	Docs        []string
}

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group.
type CoreToolDeps struct {
	WorkspaceDir string
	// GatewayVersion is the running build version surfaced by gateway status.
	// It is threaded from the server composition root so bootstrap does not
	// reach into concrete tool implementation packages.
	GatewayVersion string
	// PhoneActionSender delivers a validated phone action to the native app for
	// in-app Intent execution (the phone_write Intent ops). nil = no app channel
	// wired, so those actions report unavailable instead of dropping silently.
	PhoneActionSender PhoneActionFunc
	// WorkstationCommandSender delivers a validated workspace command (open/
	// split/close/focus/layout/wiki) to connected desktop workstations over the
	// events push channel. nil = no desktop channel wired, so the workstation
	// tool reports unavailable.
	WorkstationCommandSender func(ctx context.Context, action string, args map[string]string) error
	// WorkstationUsageHint surfaces the adoption-rate note appended to the
	// workstation tool result (효용 원장 자기조정) — "" when nothing to say.
	WorkstationUsageHint func(action string) string
	// SkillsCatalogDirs are the skill catalog roots that live outside the
	// workspace (managed ~/.deneb/skills, personal ~/.agents/skills). The
	// read tool accepts them as extra allowed roots so the SKILL.md
	// locations listed in the system prompt are actually readable —
	// without this they were clamped to the workspace root. Empty disables.
	SkillsCatalogDirs []string
	// MemoryDir is the durable memory root ({state}/memory) whose captures/
	// holds archived capture originals and oversized-document sources that
	// digest maps reference by file path. The read tool accepts it as an
	// extra allowed root so those references are actually openable — without
	// this they were clamped to the workspace root. Empty disables.
	MemoryDir string
	// BundledSkillsDir is the repo's checked-in skills/ root (SourceBundled). The
	// skills tool rejects mutating actions on paths under it so an agent can't
	// modify or delete checked-in skill files — it must create a workspace
	// override instead. Empty disables the guard.
	BundledSkillsDir string
	// FetchToolsEmbedder adds semantic ranking only inside the explicit
	// fetch_tools meta-tool. It does not alter the base tool array.
	FetchToolsEmbedder embedindex.Embedder
	// FetchToolsReranker reorders only the bounded candidates already admitted
	// by lexical/dense retrieval. nil or an error leaves that order unchanged.
	FetchToolsReranker TextReranker
	Process            ProcessDeps
	Sessions           SessionDeps
	Chrono             ChronoDeps
	Wiki               WikiDeps
	Notebook           NotebookDeps
	Contacts           ContactsDeps
	Calendar           CalendarDeps
	// MailStore is the local mail archive mirror backing the mail_archive tool's
	// storage-first read path (no per-call IMAP round-trip). nil = IMAP only.
	MailStore *mailstore.Store
	// ObserveTool is the pre-wired observe tool from the composition root
	// (server). Keeping concrete observe/workfeed/agentlog types out of this
	// package is intentional dependency inversion.
	ObserveTool ObserveToolFunc
	// SpilloverStore spills large tool results to disk; nil disables.
	SpilloverStore SpilloverStore

	// WorkFeedRW is the workfeed tool's read/settle surface. Wired to the
	// server's native-sync-teeing wrapper (NOT the raw store) so an
	// agent-side read/ack mirrors to the phone exactly like a tap in the app.
	// nil disables the workfeed tool.
	WorkFeedRW WorkFeedRW

	// MarketSummary is shared with the miniapp 오늘 dashboard so both surfaces
	// serve one cache/asOf. nil disables the market tool.
	MarketSummary func(ctx context.Context) (quotes []MarketQuote, asOf int64, stale bool, err error)

	// AsrHotwords supplies the wiki+contacts proper-noun bias for the
	// transcribe tool (people/companies/deals — same hints the capture RPC
	// uses). nil just skips the bias; the tool still works.
	AsrHotwords func() string

	// VllmBaseURLs lazily lists the deduped base URLs of OpenAI-mode vLLM
	// roles; the observe tool scrapes each endpoint's /metrics for the
	// engine-level prefix-cache hit rate. Nil disables the scrape.
	VllmBaseURLs func() []string

	// SessionMemoryFn returns session memory content for a given session key.
	// Nil means no session memory is available.
	SessionMemoryFn func(sessionKey string) string

	// Fleet wires the agent's fleet tool to the SparkFleet control plane (the
	// same passthrough the native app uses). A nil BaseURL — or one returning ""
	// — disables fleet management; the tool reports the integration is off.
	Fleet FleetDeps

	// Browser wires the agent's browser tool to a workstation Page Agent bridge
	// (scripts/dev/page-agent-bridge). A nil BaseURL — or one returning "" —
	// disables browser control; the tool reports the integration is off.
	Browser BrowserDeps

	// FilesSemanticSearch powers the files tool's semantic (vector) search mode.
	// The server owns the embedding client + index and injects this closure; nil
	// degrades the files tool's semantic=true to a name/content search, so the
	// feature is optional, not load-bearing.
	FilesSemanticSearch func(ctx context.Context, query string, max int) ([]FileHit, error)

	// ConsultPanel fans a single prompt out to the healthy models the wormhole
	// router serves, in parallel, returning each model's answer — the engine
	// behind the research_panel tool / deep-research skill. The server owns the
	// model registry + wormhole client and injects this closure; nil disables
	// the research_panel tool.
	ConsultPanel func(ctx context.Context, system, prompt string, models []string) []PanelAnswer
}

// PanelAnswer is one model's answer in a research-panel fan-out (research_panel
// tool / deep-research skill). A non-empty Err (with empty Answer) means that
// model errored or timed out and was dropped from synthesis.
type PanelAnswer struct {
	Model  string // served model name (as the wormhole router routes it)
	Family string // coarse provider family, for cross-family agreement weighting
	Answer string // the model's answer; empty when Err is set
	Ms     int64  // wall-clock ms for this model's call
	Err    string // non-empty when the model errored / timed out
}

// FleetDeps gives the fleet tool the SparkFleet base URL + optional API token.
// These mirror the gateway's sparkfleet.Client accessors so the chat tool and
// the /api/v1/fleet passthrough reach the control plane the same way.
type FleetDeps struct {
	BaseURL func() string // SparkFleet base, e.g. http://127.0.0.1:18900; "" = off
	Token   func() string // sent as X-Fleet-Token when non-empty
}

// BrowserDeps gives the browser tool the Page Agent bridge base URL + token.
// Set from DENEB_BROWSER_URL / DENEB_BROWSER_TOKEN on the gateway host.
type BrowserDeps struct {
	BaseURL func() string // bridge base, e.g. http://100.x.x.x:38401; "" = off
	Token   func() string // Bearer / X-Deneb-Browser-Token when non-empty
}

// WorkFeedRW is the mutating slice of the work-feed store the workfeed tool
// needs: list, read-stamp, ack, and publish (append an agent-authored deliverable
// card). Satisfied by the server's nativeWorkFeedStore wrapper (which tees each
// mutation onto the native-sync stream so a published card reaches the phone) —
// an interface here so tools/ never depends on the server package.
type WorkFeedRW interface {
	List(limit int, includeAcked bool) ([]WorkFeedItem, int, error)
	MarkRead(id string) (WorkFeedItem, error)
	Ack(id string) (WorkFeedItem, error)
	// Append publishes a new card the agent authored (workfeed action="publish").
	Append(item WorkFeedItem) (WorkFeedItem, error)
}

// ProcessDeps holds dependencies for exec and process management tools.
type ProcessDeps struct {
	Mgr          *process.Manager
	WorkspaceDir string
}

// SessionDeps holds dependencies for session management tools.
type SessionDeps struct {
	Manager    *session.Manager
	Transcript TranscriptStore
	// SendFn sends a message to a target session, triggering an agent run.
	SendFn func(sessionKey, message string) error
	// SubagentDefaultModel is the default model for sub-agent sessions
	// (from agents.defaults.subagents.model in deneb.json).
	SubagentDefaultModel string
	// CodingDefaultModel is non-empty when the operator configured the
	// dedicated coding role. Implementer sub-agents use the "coding" role by
	// default so code edits can run on the coding-specialized model.
	CodingDefaultModel string
	// CodingDefaultModelFn returns the live coding role binding. When set it
	// supersedes CodingDefaultModel so Settings changes take effect without a
	// gateway restart.
	CodingDefaultModelFn func() string
	// AgentLog powers action=stats (per-session run/token roll-ups). nil
	// degrades stats to an "not wired" notice; other actions are unaffected.
	AgentLog AgentLogStats
}

// ChronoDeps holds dependencies for the cron scheduling tool.
type ChronoDeps struct {
	Service *cron.Service          // persistent cron service
	RunLog  *cron.PersistentRunLog // run history
	// SendFn sends a message to a target session, triggering an agent run.
	SendFn func(sessionKey, message string) error
}

// WikiDeps holds dependencies for the wiki knowledge base tool.
type WikiDeps struct {
	Store *wiki.Store // may be nil when wiki is not enabled
	// Contacts is the device address book, used at write time to auto-record a
	// referenced person's phone/email/org into their 인물 page. May be nil when
	// the contacts store failed to init; enrichment is simply skipped then.
	Contacts ContactsBook
}

// ContactsDeps holds dependencies for the contacts address-book tool.
type ContactsDeps struct {
	Store ContactsBook // may be nil when the contacts store failed to init
}

// NotebookDeps holds dependencies for the notebook tool (NotebookLM-style
// scoped source collections for grounded, cited synthesis). Wiki reads pinned
// wiki-page sources live at brief time. Either may be nil (feature off / no
// wiki); a nil Store disables the tool, a nil Wiki just makes wiki sources
// unreadable (note sources still work).
type NotebookDeps struct {
	Store *notebook.Store
	Wiki  *wiki.Store
	// Optional ingesters for external source kinds (snapshot to text at add
	// time); nil disables that kind (the tool reports it is unwired). Wired by
	// the server from web/mail/diary infra. The file kind (PDF/image OCR, text
	// read) is handled in-package and needs no reader here.
	FetchURL  SourceReader // kind=url   — fetch + extract readable web text
	ReadMail  SourceReader // kind=mail  — read a mail thread/message by id
	ReadDiary SourceReader // kind=diary — read a diary entry by date/id
}

// SourceReader ingests an external notebook source (url/mail/diary) into text
// at add time, returning the readable text or an error. A nil reader on
// NotebookDeps disables that source kind.
type SourceReader func(ctx context.Context, ref string) (string, error)

// CalendarReader is the read-only slice of the Google calendar client the agent
// calendar tool uses. Mirrors the miniapp handler's CalendarClient — Google
// writes need an OAuth scope we don't require, so the tool only reads from Google.
type CalendarReader interface {
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]CalendarEvent, error)
	Get(ctx context.Context, eventID string) (*CalendarEvent, error)
}

// LocalCalendar is the read/write local store slice — the writable half of the
// hybrid calendar. Same interface the miniapp calendar handler depends on,
// expressed in tooldeps DTOs so this package does not import platform/calendar
// or platform/localcal.
type LocalCalendar interface {
	ListRange(from, to time.Time) []CalendarEvent
	Get(id string) *CalendarEvent
	Create(in CalendarCreateInput) (CalendarEvent, error)
	Update(id string, in CalendarCreateInput) (*CalendarEvent, error)
	Delete(id string) error
}

// CalendarWriter mirrors local calendar mutations out to an external calendar
// (Google) when the one-way write sync is enabled. Best-effort by contract: the
// caller ignores its errors because the local write is already the source of
// truth. Same shape as the miniapp handler's writer, expressed in tooldeps DTOs
// so this package does not import platform/calwrite.
type CalendarWriter interface {
	Push(ctx context.Context, localID string, ev CalendarEvent) error
	Remove(ctx context.Context, localID string) error
}

// CalendarDeps holds dependencies for the calendar agent tool. Either field may
// be nil: reads merge the read-only Google client (when OAuth is configured) with
// the local store; writes always land in the local store (so create/edit/delete
// work without a Google write scope). Both nil → the tool is not registered.
type CalendarDeps struct {
	// Client is a lazy factory for the read-only Google client (nil-safe: a
	// gateway with no OAuth tokens returns an error here and the tool degrades
	// to local-only). Matches the resolver shape in method_registry.go.
	Client func() (CalendarReader, error)
	Local  LocalCalendar
	// Writer, when set, mirrors this tool's create/update/delete out to Google —
	// the SAME mirror the miniapp calendar RPC uses. It exists because chat is
	// this agent's primary interface: without it an event created in the app UI
	// reached Google while the identical event created by saying "내일 3시 미팅
	// 잡아줘" silently did not. A nil field or a factory error = local-only.
	Writer func() (CalendarWriter, error)
}
