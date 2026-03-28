// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/node"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Server is the main gateway server.
type Server struct {
	addr             string
	httpServer       *http.Server
	dispatcher       *rpc.Dispatcher
	sessions         *session.Manager
	channels         *channel.Registry
	channelLifecycle *channel.LifecycleManager
	keyCache         *session.KeyCache
	dedupe           *dedupe.Tracker
	broadcaster      *events.Broadcaster
	processes        *process.Manager
	cron             *cron.Scheduler
	cronRunLog       *cron.PersistentRunLog
	daemon           *daemon.Daemon
	hooks            *hooks.Registry
	runtimeCfg       *config.GatewayRuntimeConfig
	authValidator    *auth.Validator
	clients          sync.Map     // connID → *WsClient; concurrent-safe client tracking
	clientCnt        atomic.Int32 // current WebSocket connection count (capped at maxWebSocketClients)
	startedAt        time.Time
	version          string
	rustFFI          bool // true when Rust FFI is available
	logColor         bool // true when ANSI color output is enabled
	logger           *slog.Logger
	ready            atomic.Bool
	shutdownOnce     sync.Once

	// Phase 2 additions.
	gatewaySubs     *events.GatewayEventSubscriptions
	chatHandler     *chat.Handler
	providers       *provider.Registry
	authManager     *provider.AuthManager
	transcript      *transcript.Writer
	authRateLimiter *auth.AuthRateLimiter
	watchdog        *monitoring.Watchdog
	channelHealth   *monitoring.ChannelHealthMonitor
	activity        *monitoring.ActivityTracker
	channelEvents   *monitoring.ChannelEventTracker
	vegaBackend     vega.Backend
	geminiEmbedder  *embedding.GeminiEmbedder
	jinaAPIKey      string

	// Phase 3: Advanced workflow subsystems.
	approvals *approval.Store
	nodes     *node.Manager
	devices   *device.Manager
	agents    *agent.Store
	skills    *skill.Manager
	wizardEng *wizard.Engine
	secrets   *secret.Resolver
	talkState *talk.State

	// Phase 4: Native system methods (migrated from bridge).
	usageTracker *usage.Tracker
	maintRunner  *maintenance.Runner
	telegramPlug *telegram.Plugin

	// Phase 4: Native agent execution.
	jobTracker *agent.JobTracker

	// Phase 5: Enhanced RPC subsystems.
	heartbeatState *rpc.HeartbeatState
	presenceStore  *rpc.PresenceStore

	// Phase 5: Plugin full registry (discovery, manifests, hooks).
	pluginFullRegistry *plugin.FullRegistry

	// Phase 5: HTTP routing for plugins.
	pluginRouter *PluginHTTPRouter

	// Phase 5: Hooks HTTP webhook handler.
	hooksHTTP *HooksHTTPHandler

	// ACP subsystem.
	acpDeps           *rpc.ACPDeps
	acpLifecycleUnsub func()

	// AuroraDream: memory consolidation lifecycle.
	autonomousSvc   *autonomous.Service
	dreamingAdapter *memory.DreamingAdapter // stored in phase 2, wired to autonomous svc

	// toolDeps holds core tool dependencies; stored on the server so late-binding
	// fields can be set from other init phases.
	toolDeps *chat.CoreToolDeps

	// GmailPoll: periodic Gmail polling with LLM analysis.
	gmailPollSvc *gmailpoll.Service
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
		addr:     addr,
		sessions: session.NewManager(),
		channels: channel.NewRegistry(),
		rustFFI:  ffi.Available,
		dedupe: dedupe.NewTracker(
			time.Duration(protocol.DedupeTTLMs)*time.Millisecond,
			protocol.DedupeMax,
		),
		version: "0.1.0-go",
		logger:  slog.New(slog.NewJSONHandler(os.Stderr, nil)),
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
	s.processes = process.NewManager(s.logger)
	s.cron = cron.NewScheduler(s.logger)
	if homeDir, err := os.UserHomeDir(); err == nil {
		s.cronRunLog = cron.NewPersistentRunLog(cron.DefaultCronStorePath(homeDir))
	}
	s.hooks = hooks.NewRegistry(s.logger)
	s.channelLifecycle = channel.NewLifecycleManager(s.channels, s.logger)
	s.activity = monitoring.NewActivityTracker()
	s.channelEvents = monitoring.NewChannelEventTracker()
	s.authRateLimiter = auth.NewAuthRateLimiter(10, 60*1000, 5*60*1000)

	// Provider auth manager.
	if s.providers != nil {
		s.authManager = provider.NewAuthManager(s.providers, s.logger)
	}

	// Phase 3: Advanced workflow subsystems.
	s.approvals = approval.NewStore()
	s.nodes = node.NewManager()
	s.devices = device.NewManager()
	s.agents = agent.NewStore()
	s.skills = skill.NewManager()
	s.wizardEng = wizard.NewEngine()
	s.secrets = secret.NewResolver()
	s.talkState = talk.NewState()
	s.jobTracker = agent.NewJobTracker(s.logger)

	// Phase 4: Native system methods (migrated from bridge).
	s.usageTracker = usage.New()
	denebDir := resolveDenebDir()
	s.maintRunner = maintenance.NewRunner(denebDir)

	// ACP subsystem: registry, bindings, persistence, lifecycle sync.
	acpRegistry := autoreply.NewACPRegistry()
	acpBindings := autoreply.NewSessionBindingService()
	acpBindingStore := autoreply.NewBindingStore(autoreply.DefaultBindingStorePath(denebDir))
	if err := acpBindingStore.RestoreToService(acpBindings); err != nil {
		s.logger.Warn("failed to restore ACP bindings", "error", err)
	}
	s.acpLifecycleUnsub = autoreply.StartACPLifecycleSync(acpRegistry, s.sessions.EventBusRef())
	s.acpDeps = &rpc.ACPDeps{
		Registry:     acpRegistry,
		Bindings:     acpBindings,
		Infra:        &autoreply.SubagentInfraDeps{ACPRegistry: acpRegistry},
		Sessions:     s.sessions,
		GatewaySubs:  s.gatewaySubs,
		BindingStore: acpBindingStore,
		Translator:   autoreply.NewACPTranslator(acpRegistry, acpBindings),
	}
	s.acpDeps.SetEnabled(true)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(metrics.RPCInstrumentation(), middleware.Logging(s.logger))
	s.registerBuiltinMethods()
	rpc.RegisterBuiltinMethods(s.dispatcher, rpc.Deps{
		Sessions:         s.sessions,
		Channels:         s.channels,
		ChannelLifecycle: s.channelLifecycle,
		GatewaySubs:      s.gatewaySubs,
		Version:          s.version,
	})
	s.registerExtendedMethods()
	s.registerPhase2Methods()
	s.registerAdvancedWorkflowMethods()
	s.registerNativeSystemMethods(denebDir)

	// Wire provider RPC methods if a provider registry is configured.
	if s.providers != nil {
		rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
			Providers: s.providers,
		})
	}

	// Initialize plugin full registry and register RPC methods.
	s.pluginFullRegistry = plugin.NewFullRegistry(s.logger)
	rpc.RegisterPluginMethods(s.dispatcher, rpc.PluginDeps{
		PluginRegistry: &pluginRegistryAdapter{registry: s.pluginFullRegistry},
	})

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
	s.pluginRouter = NewPluginHTTPRouter(s.logger, pluginAuthCheck)

	return s
}
