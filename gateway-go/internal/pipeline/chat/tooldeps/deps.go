package tooldeps

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

// TranscriptStore is the stable transcript contract owned by chatport.
type TranscriptStore = chatport.TranscriptStore

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group.
type CoreToolDeps struct {
	WorkspaceDir string
	// PhoneActionSender delivers a validated phone action to the native app for
	// in-app Intent execution (the phone_write Intent ops). nil = no app channel
	// wired, so those actions report unavailable instead of dropping silently.
	PhoneActionSender func(ctx context.Context, action string, args map[string]string) error
	// SkillsCatalogDirs are the skill catalog roots that live outside the
	// workspace (managed ~/.deneb/skills, personal ~/.agents/skills). The
	// read tool accepts them as extra allowed roots so the SKILL.md
	// locations listed in the system prompt are actually readable —
	// without this they were clamped to the workspace root. Empty disables.
	SkillsCatalogDirs []string
	// BundledSkillsDir is the repo's checked-in skills/ root (SourceBundled). The
	// skills tool rejects mutating actions on paths under it so an agent can't
	// modify or delete checked-in skill files — it must create a workspace
	// override instead. Empty disables the guard.
	BundledSkillsDir string
	Process          ProcessDeps
	Sessions         SessionDeps
	Chrono           ChronoDeps
	Wiki             WikiDeps
	Notebook         NotebookDeps
	Contacts         ContactsDeps
	Calendar         CalendarDeps
	// MailStore is the local mail archive mirror backing the mail_archive tool's
	// storage-first read path (no per-call IMAP round-trip). nil = IMAP only.
	MailStore      *mailstore.Store
	LLMClient      *llm.Client
	DefaultModel   string
	AgentLog       *agentlog.Writer
	LogCapture     *observe.LogCapture   // optional; in-memory log ring for the observe tool
	WorkFeed       *workfeed.Store       // optional; proactive-card engagement for observe action=proactive
	SpilloverStore *agent.SpilloverStore // optional; spills large tool results to disk

	// WorkFeedRW is the workfeed tool's read/settle surface. Wired to the
	// server's native-sync-teeing wrapper (NOT the raw store above) so an
	// agent-side read/ack mirrors to the phone exactly like a tap in the app.
	// nil disables the workfeed tool.
	WorkFeedRW WorkFeedRW

	// MarketSummary is market.Cache.Summary — shared with the miniapp 오늘
	// dashboard so both surfaces serve one cache/asOf. nil disables the
	// market tool.
	MarketSummary func(ctx context.Context) (quotes []market.Quote, asOf int64, stale bool, err error)

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

	// FilesSemanticSearch powers the files tool's semantic (vector) search mode.
	// The server owns the embedding client + index and injects this closure; nil
	// degrades the files tool's semantic=true to a name/content search, so the
	// feature is optional, not load-bearing.
	FilesSemanticSearch func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error)

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

// WorkFeedRW is the mutating slice of the work-feed store the workfeed tool
// needs: list, read-stamp, ack, and publish (append an agent-authored deliverable
// card). Satisfied by the server's nativeWorkFeedStore wrapper (which tees each
// mutation onto the native-sync stream so a published card reaches the phone) —
// an interface here so tools/ never depends on the server package.
type WorkFeedRW interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
	MarkRead(id string) (workfeed.Item, error)
	Ack(id string) (workfeed.Item, error)
	// Append publishes a new card the agent authored (workfeed action="publish").
	Append(item workfeed.Item) (workfeed.Item, error)
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
	AgentLog *agentlog.Writer
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
	Contacts *contacts.Store
}

// ContactsDeps holds dependencies for the contacts address-book tool.
type ContactsDeps struct {
	Store *contacts.Store // may be nil when the contacts store failed to init
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
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
	Get(ctx context.Context, eventID string) (*calendar.Event, error)
}

// LocalCalendar is the read/write local store slice — the writable half of the
// hybrid calendar. Same interface the miniapp calendar handler depends on.
type LocalCalendar interface {
	ListRange(from, to time.Time) []calendar.Event
	Get(id string) *calendar.Event
	Create(in localcal.CreateInput) (calendar.Event, error)
	Update(id string, in localcal.CreateInput) (*calendar.Event, error)
	Delete(id string) error
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
}
