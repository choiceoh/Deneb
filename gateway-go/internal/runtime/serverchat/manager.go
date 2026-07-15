// Package serverchat is the composition-root feature package for the chat
// pipeline: session lifecycle, the chat handler + tool deps, model registry,
// cron/hook scheduling, and ACP wiring. It never imports runtime/server —
// cross-cutting state it needs from its siblings (mail, auto) comes through
// serverport.Host, plus a direct reference to servermail.Manager for the
// mail-poller callback wiring chat's config assembly owns.
package serverchat

import (
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/servermail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
)

// Manager owns the chat pipeline, session lifecycle, and cron/hook
// subsystems (merged from the legacy ChatManager + SessionManager +
// HookManager). Its exported fields are set as each piece comes online
// during composition-root boot and read directly by the composition root
// (runtime/server) — only lateral reads from servermail/serverauto go
// through serverport.Host.
type Manager struct {
	Host serverport.Host

	// Mail is a direct reference to the sibling mail/calendar/phone feature
	// package. Chat's config assembly (server_chat_config.go, chat_pipeline.go)
	// wires several mail-owned callbacks (mail-analysis sinks, phone dispatch,
	// wiki merge) into chat-owned poller/tool configs — narrow enough that
	// routing every one through Host would just be indirection for its own
	// sake. One-directional: servermail never imports serverchat.
	Mail *servermail.Manager

	ChatHandler     *chat.Handler
	ToolDeps        *chat.CoreToolDeps
	ModelRegistry   *modelrole.Registry
	LocalAIHub      *localai.Hub
	EmbeddingClient *embedding.Client

	// FileSemindex owns the shared on-box store and semantic sidecar.
	FileSemindex *filesemindex.Service

	// ProactiveRelay delivers agent-initiated messages (cron results,
	// gmail poll summaries, wiki dreaming notifications) to the user's
	// channel without routing through the LLM. Set once the transcript
	// store is available.
	ProactiveRelay proactive.Relay

	// ExternalMCP holds one shared client per configured external MCP server
	// (DENEB_MCP_SERVERS), keyed by server name. Populated synchronously at
	// boot and read-only afterwards.
	ExternalMCP map[string]*mcpclient.Client

	CronRunLog  *cron.PersistentRunLog
	CronService *cron.Service

	// ChatToolRegistry is the chat pipeline's tool registry, captured at
	// InitToolsAndDeps time so background services (Plaud recording poll) can
	// execute registered tools outside chat turns.
	ChatToolRegistry *chat.ToolRegistry

	// SpilloverLifecycleUnsub unsubscribes the session-end spillover cleanup
	// hook (server_spillover_lifecycle.go); invoked on shutdown by the
	// composition root.
	SpilloverLifecycleUnsub func()

	// CheckpointLifecycleUnsub unsubscribes the session-end checkpoint cleanup
	// hook (server_checkpoint_lifecycle.go); invoked on shutdown by the
	// composition root.
	CheckpointLifecycleUnsub func()

	// ACPDeps, and the ACP-related lifecycle unsub handles, are set during
	// wireSessionACP (server_rpc_session.go) and read by the composition
	// root's shutdown path (server_lifecycle.go) and method_registry_wire.go.
	ACPDeps                 *handlerprocess.ACPDeps
	ACPLifecycleUnsub       func()
	ACPResultInjectionUnsub func()
	SnapshotLifecycleUnsub  func()

	Sessions *session.Manager

	// Autoreply session subsystems.
	AbortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	HistoryTracker *arSession.HistoryTracker // per-session conversation history
	SessionUsage   *arSession.SessionUsage   // aggregate token usage for /status reporting

	// PolarisStore is the compaction summary store, created lazily in
	// openPolarisTranscriptBridge; read by the opt-in compaction tuner.
	PolarisStore *polaris.Store

	// MailIngestHealth stores mailIngestHealth (server_chat_config.go) when
	// LMTP ingest is enabled so /health can report it without a live poll.
	MailIngestHealth     atomic.Value
	MailIngestQueueStats func() map[string]int
}

// New creates a Manager bound to host and its mail sibling.
func New(host serverport.Host, mail *servermail.Manager) *Manager {
	return &Manager{Host: host, Mail: mail}
}

// ExternalMCPClient returns the shared client for a configured external MCP
// server, or nil when that server is not configured. The client is safe for
// concurrent use; discovery state does not matter (calls lazily (re)spawn).
func (m *Manager) ExternalMCPClient(name string) *mcpclient.Client {
	return m.ExternalMCP[name]
}
