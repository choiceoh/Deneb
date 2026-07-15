// Package server implements the HTTP + SSE gateway server.
//
// Handles health endpoints, SSE streams (chat/stream, events), RPC dispatch,
// OpenAI-compatible HTTP APIs, hooks webhooks, session management, and the
// miniapp client surface. Transport is SSE, not WebSocket.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	handlerwire "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// ServerTransport owns HTTP lifecycle and connection state.
type ServerTransport struct {
	addr       string
	httpServer *http.Server
	startedAt  time.Time

	// boundAddr holds the resolved listen address (host:port) once the
	// HTTP listener binds. Set in Run(); read by background tasks like
	// the notify service self-poll. atomic.Pointer keeps reads lock-free
	// and safe across goroutines.
	boundAddr atomic.Pointer[string]
}

// BoundAddr returns the resolved listen address (e.g. "127.0.0.1:18789")
// once Run() has bound the listener, or "" before. Callers that depend
// on the address must tolerate the empty case during startup.
func (s *Server) BoundAddr() string {
	if s == nil || s.ServerTransport == nil {
		return ""
	}
	p := s.boundAddr.Load()
	if p == nil {
		return ""
	}
	return *p
}

// ServerRPC owns dispatcher construction and RPC wiring state.
type ServerRPC struct {
	dispatcher               *rpc.Dispatcher
	providers                *aibind.ProviderRegistry
	authManager              *aibind.AuthManager
	providerRuntime          *aibind.ProviderRuntimeResolver
	acpDeps                  *handlerwire.ProcessACPDeps
	acpLifecycleUnsub        func()
	acpResultInjectionUnsub  func()
	snapshotLifecycleUnsub   func()
	checkpointLifecycleUnsub func()
	spilloverLifecycleUnsub  func()
}

// ServerRuntime owns long-running runtime health/activity trackers.
type ServerRuntime struct {
	ready        atomic.Bool
	shutdownOnce sync.Once
	gatewaySubs  *svcbind.GatewayEventSubscriptions
	activity     *domainbind.ActivityTracker
	// Auto-resume state: the marker store persists "run active at T"
	// records across gateway restarts. See auto_resume.go for the
	// resume policy and file layout. resumeMu guards markerStore's
	// lazy init, and runMarkerUnsub tears down the lifecycle listener
	// on shutdown.
	resumeMu       sync.Mutex
	markerStore    *svcbind.RunMarkerStore
	runMarkerUnsub func()

	// cacheHealth holds the rolling vLLM prefix-cache hit-ratio samples surfaced
	// on /health and /status. gpuHealth caches the latest nvidia-smi reading for
	// the /health gpu section and /health/gpu route. Both zero values are
	// ready-to-use (no constructor) and degrade silently on hosts without a vLLM
	// role or NVIDIA GPU. See health_cache.go and health_gpu.go.
	healthProbes svcbind.Probes
}

// Server is the main gateway server.
type Server struct {
	*ServerTransport
	*ServerRPC
	*ServerRuntime

	// Decomposed from ServerIntegrations — each independently constructable/testable.
	*WorkflowSubsystem
	*MemorySubsystem
	*AutonomousSubsystem
	*InfraSubsystem
	*GenesisSubsystem

	broadcaster *svcbind.Broadcaster
	publisher   *svcbind.Publisher
	processes   *infrabind.Manager
	daemon      *domainbind.Daemon

	// pushHub fans proactive 업무-topic reports out to connected native clients
	// over their long-lived SSE connection (GET /api/v1/miniapp/events). Created
	// in New so it's non-nil before any handler or relay touches it.
	pushHub *svcbind.Hub

	// phoneActions correlates dispatched phone_write actions with the app's
	// execution reports (phone_action_result events) so the tool can return
	// confirmed/failed/unconfirmed instead of fire-and-forget. Created in New
	// alongside pushHub. See server_phone_action.go.
	phoneActions *phoneActionAwaiter

	// phoneEventLedger is the shared notification raw ledger — both phone-event
	// entry doors (RPC bridge + HTTP loopback) record into one instance. The
	// HTTP door (/api/event/ingest) builds its handler per request, so the lazy
	// init is guarded by phoneEventLedgerOnce (concurrent ingests must share one
	// ledger, not race two into existence — server_phone_action.go).
	phoneEventLedger     *svcbind.Ledger
	phoneEventLedgerOnce sync.Once

	// siteVisitRecorder logs project 현장 visits from phone location fixes
	// (svcbind.SiteVisitRecorder). Lazily created (guarded by
	// siteVisitRecorderOnce — same per-request door as the ledger); nil when no
	// wiki store.
	siteVisitRecorder     *svcbind.SiteVisitRecorder
	siteVisitRecorderOnce sync.Once

	// alertGate is the process-wide cooldown shared by external Fleet alerts and
	// the observatory watchdog. One instance for the server lifetime prevents a
	// route rebuild or repeated watchdog tick from resetting suppression state.
	alertGate *svcbind.AlertGate

	// pushTokenStore holds native-client FCM registration IDs (durable). The
	// registration RPCs (miniapp.push.register/unregister) write here regardless
	// of whether FCM sending is configured, so tokens accumulate and proactive
	// FCM delivery begins the moment credentials are provisioned. Created in
	// registerEarlyMethods.
	pushTokenStore *domainbind.PushStore
	// pushNotifier sends proactive notifications via FCM as a fallback when no
	// native client holds a live SSE connection (app fully closed / Doze). nil
	// (dormant) unless DENEB_FCM_CREDENTIALS_FILE points at a valid service
	// account — see internal/domain/push.
	pushNotifier *domainbind.Notifier

	runtimeCfg *infrabind.GatewayRuntimeConfig
	version    string
	logColor   bool // true when ANSI color output is enabled
	logger     *slog.Logger

	// fleet reads the SparkFleet control plane (GET /api/services) to surface
	// which GPU backends (OCR/ASR/embeddings/vLLM) are actually up instead of
	// degrading silently. nil unless DENEB_SPARKFLEET_URL is set.
	fleet *infrabind.SparkFleetClient

	// insights aggregates session/usage data for /insights reports.
	// Created during registerEarlyMethods; nil until then.
	insights *svcbind.Engine

	// polarisStore is the compaction summary store, created in
	// registerSessionRPCMethods (Session phase) and read by the opt-in
	// compaction tuner registered in registerWorkflowSideEffects (later phase).
	polarisStore *pipebind.Store

	// mailStore is the local file-backed mail archive mirror (created in
	// initMemorySubsystem). LMTP intake writes new mail to it; the mail_archive
	// tool reads from it (IMAP fallback on miss). nil = IMAP only.
	mailStore *platbind.MailStore

	// notify mirrors user-impacting error events and status snapshots to the
	// native client (live push) and the operator log. Created during
	// registerEarlyMethods.
	notify *svcbind.NotifyService

	// calendarBriefing is the D-15min meeting push service, delivered to the
	// native client. nil when calendar OAuth tokens aren't configured — safe to
	// call start() unconditionally; the service is a no-op.
	calendarBriefing *svcbind.CalendarBriefingService

	// meetingHarvest asks "회의 어떻게 됐어요?" after work-linked calendar events
	// end, pulling meeting/call outcomes into the wiki flywheel (mail is only
	// half the negotiation). nil-safe start(); see meeting_harvest.go.
	meetingHarvest *svcbind.HarvestService

	// plaudRecordings analyzes new Plaud meeting recordings via the external
	// MCP tools (transcript → meeting report → 회의록 wiki page + feed card).
	// nil-safe start(); see plaud_recordings.go.
	plaudRecordings *svcbind.PlaudService

	// chatToolRegistry is the chat pipeline's tool registry, captured at
	// pipeline build so background services (plaud recordings) can execute
	// registered tools outside a chat turn. nil until buildChatPipeline runs.
	chatToolRegistry *pipebind.ToolRegistry

	// mailIngestHealth stores mailIngestHealth when LMTP ingest is enabled so
	// /health exposes archive-context degradation instead of leaving it in logs.
	mailIngestHealth     atomic.Value
	mailIngestQueueStats func() map[string]int

	// logSwap wraps the gateway logger so the notify service can install
	// an ERROR-mirroring handler after creation. Set once in New(); never
	// nil if logger is non-nil.
	logSwap *svcbind.SwappableHandler

	// logCapture heads the slog handler chain: it mirrors every record into an
	// in-memory ring for the observe plane (infrabind.logs / infrabind.turn) before
	// delegating onward. Set once in New() alongside logSwap; nil only when
	// logger is nil.
	logCapture *infrabind.LogCapture

	// denebDir holds the resolved state directory for the lifetime of the
	// server (set in Run before registerSessionRPCMethods). Downstream
	// wiring (checkpoint root, log dir, etc.) reads this instead of
	// re-resolving — single source of truth.
	denebDir string

	// promptStore persists operator-editable prompt overrides surfaced in the
	// native Settings prompt corner. nil only if initialization is skipped in tests.
	promptStore *domainbind.PromptsStore

	// Session, chat, and hook subsystems — logically grouped to reduce God-Object growth.
	*SessionManager // sessions, transcript
	*ChatManager    // chatHandler, toolDeps, modelRegistry
	*HookManager    // hooks, cron, cronRunLog

	// lifecycleCtx is cancelled by doShutdown() so background goroutines
	// exit promptly even if the caller's original context is still alive.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc

	// bgWg tracks background goroutines launched via safeGo so that
	// shutdown can wait for them to finish before exiting.
	bgWg sync.WaitGroup

	// OnListening is called after the TCP listener is bound successfully.
	// Use this to print the startup banner or signal readiness to external callers.
	OnListening func(addr net.Addr)
}

// sessionSnapshotProvider implements svcbind.SessionSnapshotProvider by
// reading from the domainbind.Manager and mapping to the events wire type.
type sessionSnapshotProvider struct {
	sessions *domainbind.Manager
}

// SessionSnapshot returns the latest observable snapshot for a session.
func (p *sessionSnapshotProvider) SessionSnapshot(sessionKey string) *svcbind.SessionSnapshot {
	s := p.sessions.Get(sessionKey)
	if s == nil {
		return nil
	}
	return &svcbind.SessionSnapshot{
		SessionKey:     s.Key,
		SessionID:      s.SessionID,
		Kind:           string(s.Kind),
		Channel:        s.Channel,
		Label:          s.Label,
		Status:         string(s.Status),
		Model:          s.Model,
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		RuntimeMs:      s.RuntimeMs,
		TotalTokens:    s.TotalTokens,
		AbortedLastRun: s.AbortedLastRun,
	}
}

// ShutdownCtx returns the server's lifecycle context, which is cancelled
// when doShutdown() runs. Background goroutines that outlive individual
// requests should derive from this so graceful shutdown does not leak them.
// Returns a non-nil context.Background before Run() has initialized the
// lifecycle context, so callers need not nil-check.
func (s *Server) ShutdownCtx() context.Context {
	if s.lifecycleCtx == nil {
		return context.Background()
	}
	return s.lifecycleCtx
}

// safeGo starts a goroutine with panic recovery that logs and continues.
// The goroutine is tracked by bgWg so shutdown can wait for completion.
func (s *Server) safeGo(name string, fn func()) {
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic in background goroutine", "goroutine", name, "panic", r)
			}
		}()
		fn()
	}()
}

// New creates a new gateway server bound to the given address.
func New(addr string, opts ...Option) (*Server, error) {
	s := &Server{
		ServerTransport:     &ServerTransport{addr: addr},
		ServerRPC:           &ServerRPC{},
		ServerRuntime:       &ServerRuntime{},
		MemorySubsystem:     &MemorySubsystem{},
		AutonomousSubsystem: &AutonomousSubsystem{},
		GenesisSubsystem:    &GenesisSubsystem{},
		version:             "0.1.0-go",
		logger:              slog.Default(),
		pushHub:             svcbind.NewHub(),
		phoneActions:        newPhoneActionAwaiter(),
		alertGate:           svcbind.NewAlertGate(),
		SessionManager: &SessionManager{
			sessions:       domainbind.NewManager(),
			abortMemory:    pipebind.NewAbortMemory(2000),
			historyTracker: pipebind.NewHistoryTracker(),
			sessionUsage:   &pipebind.SessionUsage{},
		},
		ChatManager: &ChatManager{},
		HookManager: &HookManager{},
	}
	for _, opt := range opts {
		opt(s)
	}

	// Wrap s.logger with a swappable handler so the notify service can
	// later install a forwarding handler without subsystems needing to
	// re-capture the logger. Installed BEFORE the first downstream
	// capture (broadcaster, gatewaySubs, ...) so every subsystem that
	// reads s.logger sees the swappable indirection.
	if s.logger != nil {
		// Head the chain with the observe capture handler so every record is
		// mirrored into the ring, THEN make it swappable so the notify service
		// can wrap on top later. Resulting order: notify(swap) → capture → base.
		// notify forwards ERRORs but always runs its delegate first, so capture
		// still sees every line after the swap.
		s.logCapture = infrabind.NewCapture(s.logger.Handler(), infrabind.NewRing(infrabind.DefaultRingSize))
		s.logSwap = svcbind.NewSwappableHandler(s.logCapture)
		if s.logSwap != nil {
			s.logger = slog.New(s.logSwap)
		}
	}

	// Initialise the lifecycle context up front so any background goroutine
	// started during New() (e.g. checkpoint-gc) can read it race-free via
	// ShutdownCtx(). initAndListen later wires caller-ctx cancellation in
	// as a forwarder rather than replacing the context pointer.
	s.lifecycleCtx, s.lifecycleCancel = context.WithCancel(context.Background())

	s.broadcaster = svcbind.NewBroadcaster()
	s.broadcaster.SetLogger(s.logger)
	s.gatewaySubs = svcbind.NewGatewayEventSubscriptions(svcbind.GatewaySubscriptionParams{
		Broadcaster: s.broadcaster,
		Logger:      s.logger,
	})
	s.publisher = svcbind.NewPublisher(s.broadcaster, &sessionSnapshotProvider{sessions: s.sessions}, s.logger)
	s.gatewaySubs.SetPublisher(s.publisher)
	s.processes = infrabind.NewManager(s.logger)
	if homeDir, err := os.UserHomeDir(); err == nil {
		cronEnabled := true
		// Load config early just to honor a cron-disabled setting. The cron
		// delivery default (DefaultChannel/DefaultTo below) no longer depends on
		// any channel config: the Telegram bot was retired (PR #1922), so every
		// job routes to the native client's 업무 session via MainSessionHandoff.
		if snap, err := infrabind.LoadConfigFromDefaultPath(); err == nil && snap != nil {
			if snap.Config.Cron != nil && snap.Config.Cron.Enabled != nil && !*snap.Config.Cron.Enabled {
				cronEnabled = false
			}
		}
		storePath := platbind.DefaultCronStorePath(homeDir)
		s.cronRunLog = platbind.NewPersistentRunLog(storePath)
		// Cron delivery defaults: every report routes to the native client's
		// 업무 chat (client:main) via MainSessionHandoff regardless of the
		// per-job target. Default targetless jobs straight to that native
		// sentinel — without it a job with no explicit Delivery.To fails
		// ResolveDeliveryTarget ("no delivery recipient configured") before its
		// agent even runs, and with Telegram retired there is no other channel.
		s.cronService = platbind.NewCronService(platbind.ServiceConfig{
			StorePath:      storePath,
			DefaultChannel: "client",
			DefaultTo:      svcbind.NativeWorkSessionTarget,
			Enabled:        cronEnabled,
			Sessions:       s.sessions,
		}, nil, s.logger) // agent runner wired later during chat handler setup
		if !cronEnabled {
			s.logger.Info("cron service disabled by config")
		}
	}
	s.activity = domainbind.NewActivityTracker()

	// Provider auth manager and runtime resolver.
	if s.providers != nil {
		s.authManager = aibind.NewAuthManager(s.providers, s.logger)
		s.providerRuntime = aibind.NewProviderRuntimeResolver(s.providers, s.logger)
	}

	// Subsystem construction: each independently testable.
	denebDir := svcbind.DenebDir()
	s.denebDir = denebDir
	s.promptStore = newPromptStore(denebDir)
	s.InfraSubsystem = NewInfraSubsystem(s.logger, denebDir)
	s.WorkflowSubsystem = NewWorkflowSubsystem(s.logger)

	// ACP subsystem: registry, bindings, persistence, lifecycle sync.
	s.initACPSubsystem(denebDir)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(infrabind.RPCInstrumentation(), infrabind.Logging(s.logger))

	// Build GatewayHub — central service registry. Chat is nil until
	// registerSessionRPCMethods() creates the chat handler.
	hub := s.buildHub()

	s.registerBuiltinMethods()
	rpc.RegisterBuiltinMethods(s.dispatcher)
	if err := s.registerEarlyMethods(hub, denebDir); err != nil {
		return nil, fmt.Errorf("register early methods: %w", err)
	}
	s.registerSessionRPCMethods() // chat pipeline init + handler creation
	if s.localAIHub != nil {
		pipebind.SetLocalAIHub(s.localAIHub)
	}
	hub.AdvancePhase(rpcutil.PhaseSession) // mark chatHandler as available
	s.initGenesisServices()                // create genesis deps (before late methods for Rule 1)
	s.registerLateMethods(hub)             // Chat-dependent domains
	s.registerWorkflowSideEffects(hub)     // non-RPC: autonomous, dreaming, notifier

	// One-shot GC of long-abandoned checkpoint sessions. Runs in the
	// background so startup latency is unaffected, and cancels cleanly if
	// the server shuts down mid-scan. 30-day cutoff keeps retention
	// generous — per-file/per-session caps already handle the common case;
	// this only reclaims directories belonging to sessions that will never
	// be resumed.
	s.safeGo("checkpoint-gc", func() {
		cpRoot := filepath.Join(denebDir, "checkpoints")
		res, err := checkpoint.CleanupStaleSessions(s.ShutdownCtx(), cpRoot, 30*24*time.Hour)
		if err != nil {
			s.logger.Warn("checkpoint gc failed", "root", cpRoot, "error", err)
			return
		}
		if res.Removed > 0 {
			s.logger.Info("checkpoint gc removed stale sessions",
				"root", cpRoot,
				"scanned", res.Scanned,
				"removed", res.Removed,
				"freedBytes", res.RemovedBytes)
		}
		for _, e := range res.Errors {
			s.logger.Warn("checkpoint gc: per-session error", "error", e)
		}
	})

	// Persist "run active" markers on session state transitions so the
	// auto-resume subsystem can recover runs that a gateway crash or
	// restart interrupted. See auto_resume.go for the resume policy.
	s.runMarkerUnsub = s.initRunMarkerLifecycle()

	// SparkFleet control-plane discovery (opt-in via DENEB_SPARKFLEET_URL): surface
	// which GPU backends are actually up instead of degrading silently. New returns
	// nil when the env var is unset, so this stays a no-op unless configured.
	fleetURL := os.Getenv("DENEB_SPARKFLEET_URL")
	fleetLogger := s.logger
	if fleetURL != "" {
		fleetLogger = sparkFleetLogger(s.logger) // dedicated file, off the main gateway log
	}
	s.fleet = infrabind.NewSparkFleetClient(fleetURL, fleetLogger)

	return s, nil
}

// sparkFleetLogger returns a logger that writes SparkFleet control-plane chatter
// (GPU backend up/down churn) to its own file — <stateDir>/logs/infrabind.log —
// off the main gateway log. That backend status is SparkFleet's domain and
// flooded journald (1000+ lines/day when a backend is persistently down); the
// live status is still on /health. Falls back to a discard logger (never the
// main one) if the file can't be opened, so the noise can never return to the
// main stream. The handle lives for the process; the SparkFleet client only logs
// on state transitions, so the file grows slowly.
func sparkFleetLogger(main *slog.Logger) *slog.Logger {
	dir := filepath.Join(infrabind.ResolveStateDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		main.Warn("sparkfleet: dedicated log dir unavailable; suppressing sparkfleet logs", "error", err)
		return slog.New(slog.DiscardHandler)
	}
	f, err := os.OpenFile(filepath.Join(dir, "infrabind.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		main.Warn("sparkfleet: dedicated log file unavailable; suppressing sparkfleet logs", "error", err)
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
