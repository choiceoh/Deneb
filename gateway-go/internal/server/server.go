// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rl"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	handlerbridge "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/bridge"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ServerTransport owns HTTP/WS lifecycle and connection state.
type ServerTransport struct {
	addr       string
	httpServer *http.Server
	clients    sync.Map     // connID → *WsClient; concurrent-safe client tracking
	clientCnt  atomic.Int32 // current WebSocket connection count (capped at maxWebSocketClients)
	startedAt  time.Time
}

// ServerRPC owns dispatcher construction and RPC/auth wiring state.
type ServerRPC struct {
	dispatcher              *rpc.Dispatcher
	authValidator           *auth.Validator
	providers               *provider.Registry
	authManager             *provider.AuthManager
	providerRuntime         *provider.ProviderRuntimeResolver
	authRateLimiter         *auth.AuthRateLimiter
	acpDeps                 *handlerprocess.ACPDeps
	acpLifecycleUnsub       func()
	acpResultInjectionUnsub func()
	snapshotLifecycleUnsub  func()
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

	dedupe      *dedupe.Tracker
	broadcaster *events.Broadcaster
	publisher   *events.Publisher
	processes   *process.Manager
	daemon      *daemon.Daemon
	runtimeCfg    *config.GatewayRuntimeConfig
	configWatcher *config.Watcher
	version       string
	rustFFI     bool // true when Rust FFI is available
	logColor    bool // true when ANSI color output is enabled
	logger      *slog.Logger

	// Session, chat, and hook subsystems — logically grouped to reduce God-Object growth.
	*SessionManager // sessions, keyCache, transcript, presenceStore, heartbeatState
	*ChatManager    // chatHandler, toolDeps, telegramPlug
	*HookManager    // hooks, hooksHTTP, cron, cronRunLog

	// RL self-learning pipeline (optional, nil when rl.enable=false).
	rlService *rl.Service
	rlHook    *rl.SessionHook

	// bridgeInjector is late-bound: created in registerEarlyMethods,
	// populated in registerLateMethods after chatHandler is ready.
	bridgeInjector *handlerbridge.Injector

	// githubWebhookCfg is non-nil when GITHUB_WEBHOOK_SECRET is set.
	// Resolved once at startup from environment variables; never mutated.
	githubWebhookCfg *GitHubWebhookConfig

	// bgWg tracks background goroutines launched via safeGo so that
	// shutdown can wait for them to finish before exiting.
	bgWg sync.WaitGroup

	// OnListening is called after the TCP listener is bound successfully.
	// Use this to print the startup banner or signal readiness to external callers.
	OnListening func(addr net.Addr)
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
		rustFFI:             ffi.Available,
		dedupe: dedupe.NewTracker(
			time.Duration(protocol.DedupeTTLMs)*time.Millisecond,
			protocol.DedupeMax,
		),
		version: "0.1.0-go",
		logger:  slog.Default(),
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

	s.broadcaster = events.NewBroadcaster()
	s.broadcaster.SetLogger(s.logger)
	s.keyCache = session.NewKeyCache()
	s.gatewaySubs = events.NewGatewayEventSubscriptions(events.GatewaySubscriptionParams{
		Broadcaster: s.broadcaster,
		Logger:      s.logger,
	})
	s.publisher = events.NewPublisher(s.broadcaster, &sessionSnapshotAdapter{sessions: s.sessions}, s.logger)
	s.gatewaySubs.SetPublisher(s.publisher)
	s.processes = process.NewManager(s.logger)
	if homeDir, err := os.UserHomeDir(); err == nil {
		storePath := cron.DefaultCronStorePath(homeDir)
		s.cronRunLog = cron.NewPersistentRunLog(storePath)
		s.cronService = cron.NewService(cron.ServiceConfig{
			StorePath:      storePath,
			DefaultChannel: "telegram",
			Enabled:        true,
			Sessions:       s.sessions,
		}, nil, s.logger) // agent runner wired later during chat handler setup
	}
	s.initHooksFromConfig()

	// GitHub webhook: resolved from env vars; nil when GITHUB_WEBHOOK_SECRET is unset.
	s.githubWebhookCfg = GitHubWebhookConfigFromEnv()
	if s.githubWebhookCfg != nil {
		s.logger.Info("github webhook enabled", "chatID", s.githubWebhookCfg.ChatID != "")
	}

	s.snapshotStore = telegram.NewSnapshotStore()
	s.activity = monitoring.NewActivityTracker()
	s.channelEvents = monitoring.NewChannelEventTracker()
	s.authRateLimiter = auth.NewAuthRateLimiter(10, 60*1000, 5*60*1000)

	// Provider auth manager and runtime resolver.
	if s.providers != nil {
		s.authManager = provider.NewAuthManager(s.providers, s.logger)
		s.providerRuntime = provider.NewProviderRuntimeResolver(s.providers, s.logger)
	}

	// Subsystem construction: each independently testable.
	denebDir := resolveDenebDir()
	s.InfraSubsystem = NewInfraSubsystem(s.logger, denebDir)
	s.WorkflowSubsystem = NewWorkflowSubsystem(s.logger)

	// ACP subsystem: registry, bindings, persistence, lifecycle sync.
	s.initACPSubsystem(denebDir)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(metrics.RPCInstrumentation(), middleware.Logging(s.logger))

	// RL self-learning pipeline: create service before hub so
	// hub.RLService() is non-nil during early method registration.
	s.initRLService()

	// Build GatewayHub — central service registry. Chat is nil until
	// registerSessionRPCMethods() creates the chat handler.
	hub := s.buildHub()

	s.registerBuiltinMethods()
	if err := rpc.RegisterBuiltinMethods(s.dispatcher, rpc.Deps{
		Sessions:      s.sessions,
		SnapshotStore: s.snapshotStore,
		GatewaySubs:   s.gatewaySubs,
		Version:       s.version,
	}); err != nil {
		return nil, fmt.Errorf("register builtin methods: %w", err)
	}
	if err := s.registerEarlyMethods(hub, denebDir); err != nil {
		return nil, fmt.Errorf("register early methods: %w", err)
	}
	s.registerSessionRPCMethods() // chat pipeline init + handler creation
	if s.localAIHub != nil {
		hub.SetLocalAIHub(s.localAIHub)
	}
	hub.AdvancePhase(rpcutil.PhaseSession) // mark chatHandler as available
	s.initGenesisServices()                // create genesis deps (before late methods for Rule 1)
	s.registerLateMethods(hub)             // Chat-dependent domains
	s.registerWorkflowSideEffects(hub)     // non-RPC: autonomous, dreaming, notifier

	return s, nil
}
