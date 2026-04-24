// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
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

	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/middleware"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
)

// ServerTransport owns HTTP lifecycle and connection state.
type ServerTransport struct {
	addr       string
	httpServer *http.Server
	startedAt  time.Time
}

// ServerRPC owns dispatcher construction and RPC wiring state.
type ServerRPC struct {
	dispatcher               *rpc.Dispatcher
	providers                *provider.Registry
	authManager              *provider.AuthManager
	providerRuntime          *provider.ProviderRuntimeResolver
	acpDeps                  *handlerprocess.ACPDeps
	acpLifecycleUnsub        func()
	acpResultInjectionUnsub  func()
	snapshotLifecycleUnsub   func()
	checkpointLifecycleUnsub func()
	spilloverLifecycleUnsub  func()
}

// ServerRuntime owns long-running runtime health/activity trackers.
type ServerRuntime struct {
	ready           atomic.Bool
	shutdownOnce    sync.Once
	gatewaySubs     *events.GatewayEventSubscriptions
	channelHealth   *monitoring.ChannelHealthMonitor
	activity        *monitoring.ActivityTracker
	channelEvents   *monitoring.ChannelEventTracker
	snapshotStore   *telegram.SnapshotStore
	runStateMachine *telegram.RunStateMachine
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

	broadcaster *events.Broadcaster
	publisher   *events.Publisher
	processes   *process.Manager
	daemon      *daemon.Daemon
	runtimeCfg  *config.GatewayRuntimeConfig
	version     string
	logColor    bool // true when ANSI color output is enabled
	logger      *slog.Logger

	// insights aggregates session/usage data for /insights reports.
	// Created during registerEarlyMethods; nil until then.
	insights *insights.Engine

	// denebDir holds the resolved state directory for the lifetime of the
	// server (set in Run before registerSessionRPCMethods). Downstream
	// wiring (checkpoint root, log dir, etc.) reads this instead of
	// re-resolving — single source of truth.
	denebDir string

	// Session, chat, and hook subsystems — logically grouped to reduce God-Object growth.
	*SessionManager // sessions, transcript
	*ChatManager    // chatHandler, toolDeps, telegramPlug
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

// sessionSnapshotProvider implements events.SessionSnapshotProvider by
// reading from the session.Manager and mapping to the events wire type.
type sessionSnapshotProvider struct {
	sessions *session.Manager
}

func (p *sessionSnapshotProvider) SessionSnapshot(sessionKey string) *events.SessionSnapshot {
	s := p.sessions.Get(sessionKey)
	if s == nil {
		return nil
	}
	return &events.SessionSnapshot{
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
// insightsEngine returns the server's active insights engine, or nil if one
// has not been wired yet. Used by the /insights slash-command dispatcher.
func (s *Server) insightsEngine() *insights.Engine { return s.insights }

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
		SessionManager: &SessionManager{
			sessions:       session.NewManager(),
			abortMemory:    arSession.NewAbortMemory(2000),
			historyTracker: arSession.NewHistoryTracker(),
			sessionUsage:   &arSession.SessionUsage{},
		},
		ChatManager: &ChatManager{},
		HookManager: &HookManager{},
	}
	for _, opt := range opts {
		opt(s)
	}

	// Initialise the lifecycle context up front so any background goroutine
	// started during New() (e.g. checkpoint-gc) can read it race-free via
	// ShutdownCtx(). initAndListen later wires caller-ctx cancellation in
	// as a forwarder rather than replacing the context pointer.
	s.lifecycleCtx, s.lifecycleCancel = context.WithCancel(context.Background())

	s.broadcaster = events.NewBroadcaster()
	s.broadcaster.SetLogger(s.logger)
	s.gatewaySubs = events.NewGatewayEventSubscriptions(events.GatewaySubscriptionParams{
		Broadcaster: s.broadcaster,
		Logger:      s.logger,
	})
	s.publisher = events.NewPublisher(s.broadcaster, &sessionSnapshotProvider{sessions: s.sessions}, s.logger)
	s.gatewaySubs.SetPublisher(s.publisher)
	s.processes = process.NewManager(s.logger)
	if homeDir, err := os.UserHomeDir(); err == nil {
		cronEnabled := true
		if snap, err := config.LoadConfigFromDefaultPath(); err == nil && snap != nil {
			if snap.Config.Cron != nil && snap.Config.Cron.Enabled != nil && !*snap.Config.Cron.Enabled {
				cronEnabled = false
			}
		}
		storePath := cron.DefaultCronStorePath(homeDir)
		s.cronRunLog = cron.NewPersistentRunLog(storePath)
		s.cronService = cron.NewService(cron.ServiceConfig{
			StorePath:      storePath,
			DefaultChannel: "telegram",
			Enabled:        cronEnabled,
			Sessions:       s.sessions,
		}, nil, s.logger) // agent runner wired later during chat handler setup
		if !cronEnabled {
			s.logger.Info("cron service disabled by config")
		}
	}
	s.snapshotStore = telegram.NewSnapshotStore()
	s.activity = monitoring.NewActivityTracker()
	s.channelEvents = monitoring.NewChannelEventTracker()

	// Provider auth manager and runtime resolver.
	if s.providers != nil {
		s.authManager = provider.NewAuthManager(s.providers, s.logger)
		s.providerRuntime = provider.NewProviderRuntimeResolver(s.providers, s.logger)
	}

	// Subsystem construction: each independently testable.
	denebDir := resolveDenebDir()
	s.denebDir = denebDir
	s.InfraSubsystem = NewInfraSubsystem(s.logger, denebDir)
	s.WorkflowSubsystem = NewWorkflowSubsystem(s.logger)

	// ACP subsystem: registry, bindings, persistence, lifecycle sync.
	s.initACPSubsystem(denebDir)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(metrics.RPCInstrumentation(), middleware.Logging(s.logger))

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
		hub.SetLocalAIHub(s.localAIHub)
	}
	if s.embeddingClient != nil {
		hub.SetEmbeddingClient(s.embeddingClient)
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

	return s, nil
}
