// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/server/pluginrouter"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
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
	dispatcher             *rpc.Dispatcher
	authValidator          *auth.Validator
	providers              *provider.Registry
	authManager            *provider.AuthManager
	providerRuntime        *provider.ProviderRuntimeResolver
	authRateLimiter        *auth.AuthRateLimiter
	acpDeps                *handlerprocess.ACPDeps
	acpLifecycleUnsub      func()
	acpResultInjectionUnsub func()
	snapshotLifecycleUnsub func()
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
	*PluginSubsystem
	*MemorySubsystem
	*AutonomousSubsystem
	*InfraSubsystem

	dedupe      *dedupe.Tracker
	broadcaster *events.Broadcaster
	publisher   *events.Publisher
	processes   *process.Manager
	daemon      *daemon.Daemon
	runtimeCfg  *config.GatewayRuntimeConfig
	version     string
	rustFFI     bool // true when Rust FFI is available
	logColor    bool // true when ANSI color output is enabled
	logger      *slog.Logger

	// Session, chat, and hook subsystems — logically grouped to reduce God-Object growth.
	*SessionManager // sessions, keyCache, transcript, presenceStore, heartbeatState
	*ChatManager    // chatHandler, toolDeps, telegramPlug
	*HookManager    // hooks, hooksHTTP, cron, cronRunLog

	// githubWebhookCfg is non-nil when GITHUB_WEBHOOK_SECRET is set.
	// Resolved once at startup from environment variables; never mutated.
	githubWebhookCfg *GitHubWebhookConfig

	// OnListening is called after the TCP listener is bound successfully.
	// Use this to print the startup banner or signal readiness to external callers.
	OnListening func(addr net.Addr)
}

// safeGo starts a goroutine with panic recovery that logs and continues.
func (s *Server) safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic in background goroutine", "goroutine", name, "panic", r)
			}
		}()
		fn()
	}()
}

// Option configures the gateway server.
type Option func(*Server)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithVersion sets the server version string.
func WithVersion(version string) Option {
	return func(s *Server) {
		s.version = version
	}
}

// WithConfig sets the resolved runtime configuration.
func WithConfig(cfg *config.GatewayRuntimeConfig) Option {
	return func(s *Server) {
		s.runtimeCfg = cfg
	}
}

// WithLogColor enables ANSI color in startup/shutdown banners.
func WithLogColor(color bool) Option {
	return func(s *Server) {
		s.logColor = color
	}
}

// RuntimeConfig returns the server's runtime configuration (may be nil if not set).
func (s *Server) RuntimeConfig() *config.GatewayRuntimeConfig {
	return s.runtimeCfg
}

// DispatchRPC dispatches an RPC request through the server's dispatcher.
// This allows internal components (e.g., model prewarm) to invoke RPC
// methods without going through HTTP/WebSocket.
func (s *Server) DispatchRPC(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	return s.dispatcher.Dispatch(ctx, req)
}

// WithAuthValidator sets the auth validator for token-based authentication.
// If not set, the server operates in no-auth mode (all connections are trusted).
func WithAuthValidator(v *auth.Validator) Option {
	return func(s *Server) {
		s.authValidator = v
	}
}

// WithProviders sets the provider plugin registry.
func WithProviders(r *provider.Registry) Option {
	return func(s *Server) {
		s.providers = r
	}
}

// WithTranscript sets the session transcript writer.
func WithTranscript(w *transcript.Writer) Option {
	return func(s *Server) {
		s.transcript = w
	}
}

// WithHooksHTTP sets the hooks HTTP webhook handler.
func WithHooksHTTP(h *HooksHTTPHandler) Option {
	return func(s *Server) {
		s.hooksHTTP = h
	}
}

// WithGeminiEmbedder sets the Gemini embedder for the memory subsystem.
func WithGeminiEmbedder(e *embedding.GeminiEmbedder) Option {
	return func(s *Server) {
		s.geminiEmbedder = e
	}
}

// WithJinaAPIKey sets the Jina API key for cross-encoder reranking.
func WithJinaAPIKey(key string) Option {
	return func(s *Server) {
		s.jinaAPIKey = key
	}
}

// New creates a new gateway server bound to the given address.
func New(addr string, opts ...Option) *Server {
	s := &Server{
		ServerTransport:      &ServerTransport{addr: addr},
		ServerRPC:            &ServerRPC{},
		ServerRuntime:        &ServerRuntime{},
		PluginSubsystem:     &PluginSubsystem{},
		MemorySubsystem:     &MemorySubsystem{},
		AutonomousSubsystem: &AutonomousSubsystem{},
		rustFFI:            ffi.Available,
		dedupe: dedupe.NewTracker(
			time.Duration(protocol.DedupeTTLMs)*time.Millisecond,
			protocol.DedupeMax,
		),
		version: "0.1.0-go",
		logger:  slog.New(slog.NewJSONHandler(os.Stderr, nil)),
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
	s.cron = cron.NewScheduler(s.logger)
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
	s.hooks = hooks.NewRegistry(s.logger)
	// Load user-defined hooks from deneb.json so they fire on gateway events.
	if snap, err := config.LoadConfigFromDefaultPath(); err == nil && snap != nil && snap.Config.Hooks != nil {
		for _, entry := range snap.Config.Hooks.Entries {
			enabled := true
			if entry.Enabled != nil {
				enabled = *entry.Enabled
			}
			timeoutMs := int64(30000)
			if entry.TimeoutMs != nil {
				timeoutMs = int64(*entry.TimeoutMs)
			}
			blocking := false
			if entry.Blocking != nil {
				blocking = *entry.Blocking
			}
			if err := s.hooks.Register(hooks.Hook{
				ID:        entry.ID,
				Event:     hooks.Event(entry.Event),
				Command:   entry.Command,
				TimeoutMs: timeoutMs,
				Blocking:  blocking,
				Enabled:   enabled,
			}); err != nil {
				s.logger.Warn("failed to register hook", "id", entry.ID, "error", err)
			}
		}
		if len(snap.Config.Hooks.Entries) > 0 {
			s.logger.Info("loaded user-defined hooks from config", "count", len(snap.Config.Hooks.Entries))
		}
	}
	s.internalHooks = hooks.NewInternalRegistry(s.logger)

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
	acpRegistry := acp.NewACPRegistry()
	acpRegistry.SetSessionManager(s.sessions)
	acpBindings := acp.NewSessionBindingService()
	acpBindingStore := acp.NewBindingStore(acp.DefaultBindingStorePath(denebDir))
	if err := acpBindingStore.RestoreToService(acpBindings); err != nil {
		s.logger.Warn("failed to restore ACP bindings", "error", err)
	}
	s.acpLifecycleUnsub = acp.StartACPLifecycleSync(acpRegistry, s.sessions.EventBusRef())

	// Clear frozen context snapshots when sessions are evicted or deleted,
	// preventing stale snapshot accumulation in long-running gateways.
	s.snapshotLifecycleUnsub = s.sessions.EventBusRef().Subscribe(func(e session.Event) {
		if e.Kind == session.EventDeleted {
			prompt.ClearSessionSnapshot(e.Key)
		}
	})
	s.acpDeps = &handlerprocess.ACPDeps{
		Registry:     acpRegistry,
		Bindings:     acpBindings,
		Infra:        &acp.SubagentInfraDeps{ACPRegistry: acpRegistry, Sessions: s.sessions},
		Sessions:     s.sessions,
		GatewaySubs:  s.gatewaySubs,
		BindingStore: acpBindingStore,
		Translator:   acp.NewACPTranslator(acpRegistry, acpBindings),
	}
	s.acpDeps.SetEnabled(true)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(metrics.RPCInstrumentation(), middleware.Logging(s.logger))

	// Build GatewayHub — central service registry. Chat is nil until
	// registerSessionRPCMethods() creates the chat handler.
	hub := s.buildHub()

	s.registerBuiltinMethods()
	rpc.RegisterBuiltinMethods(s.dispatcher, rpc.Deps{
		Sessions:      s.sessions,
		SnapshotStore: s.snapshotStore,
		GatewaySubs:   s.gatewaySubs,
		Version:       s.version,
	})
	s.registerEarlyMethods(hub, denebDir)  // ~30 domains via hub accessors
	s.registerSessionRPCMethods()          // chat pipeline init + handler creation
	hub.AdvancePhase(rpcutil.PhaseSession) // mark chatHandler as available
	s.registerLateMethods(hub)             // Chat-dependent domains
	s.registerWorkflowSideEffects(hub)     // non-RPC: autonomous, dreaming, notifier

	// Initialize plugin full registry, discoverer, typed hook runner, conversation bindings, and register RPC methods.
	s.pluginFullRegistry = plugin.NewFullRegistry(s.logger)
	s.pluginDiscoverer = plugin.NewPluginDiscoverer(s.logger)
	s.conversationBindings = plugin.NewConversationBindingStore()
	// Use the FullRegistry's hook runner so plugin-registered hooks and
	// chat-fired hooks share the same TypedHookRunner instance.
	s.pluginTypedHookRunner = s.pluginFullRegistry.HookRunner()
	// Late-bind: pluginTypedHookRunner was nil when chatHandler was constructed
	// (registerSessionRPCMethods runs before plugin init). Wire it now.
	if s.chatHandler != nil {
		s.chatHandler.SetPluginHookRunner(s.pluginTypedHookRunner)
	}
	s.dispatcher.RegisterDomain(handlerskill.PluginMethods(handlerskill.PluginDeps{
		PluginRegistry: &pluginRegistryAdapter{registry: s.pluginFullRegistry},
	}))

	// Plugin HTTP router with auth check backed by the gateway auth validator.
	var pluginAuthCheck func(r *http.Request) bool
	if s.authValidator != nil {
		pluginAuthCheck = func(r *http.Request) bool {
			token := extractBearerToken(r)
			if token == "" {
				return false
			}
			_, err := s.authValidator.ValidateToken(token)
			return err == nil
		}
	}
	s.pluginRouter = pluginrouter.New(s.logger, pluginAuthCheck)

	return s
}
