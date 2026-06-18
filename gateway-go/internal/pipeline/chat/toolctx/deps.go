package toolctx

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group.
type CoreToolDeps struct {
	WorkspaceDir string
	// SkillsCatalogDirs are the skill catalog roots that live outside the
	// workspace (managed ~/.deneb/skills, personal ~/.agents/skills). The
	// read tool accepts them as extra allowed roots so the SKILL.md
	// locations listed in the system prompt are actually readable —
	// without this they were clamped to the workspace root. Empty disables.
	SkillsCatalogDirs []string
	Process           ProcessDeps
	Sessions          SessionDeps
	Chrono            ChronoDeps
	Wiki              WikiDeps
	Contacts          ContactsDeps
	Calendar          CalendarDeps
	LLMClient         *llm.Client
	DefaultModel      string
	AgentLog          *agentlog.Writer
	LogCapture        *observe.LogCapture   // optional; in-memory log ring for the observe tool
	WorkFeed          *workfeed.Store       // optional; proactive-card engagement for observe action=proactive
	SpilloverStore    *agent.SpilloverStore // optional; spills large tool results to disk

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
}

// FleetDeps gives the fleet tool the SparkFleet base URL + optional API token.
// These mirror the gateway's sparkfleet.Client accessors so the chat tool and
// the /api/v1/fleet passthrough reach the control plane the same way.
type FleetDeps struct {
	BaseURL func() string // SparkFleet base, e.g. http://127.0.0.1:18900; "" = off
	Token   func() string // sent as X-Fleet-Token when non-empty
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
