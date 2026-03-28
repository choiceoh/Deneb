// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/choiceoh/deneb/gateway-go/internal/logging"
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
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

const (
	// maxRPCBodyBytes limits the HTTP RPC request body to 1 MB.
	maxRPCBodyBytes = 1 * 1024 * 1024
	// maxWebSocketClients limits the number of concurrent WebSocket connections.
	maxWebSocketClients = 256
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
	discordPlug  *discord.Plugin

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

// initAndListen creates the HTTP server, binds to the address, and starts
// background subsystems (tick broadcaster, monitoring, process pruner, hooks).
// Shared by Run and StartAndListen to avoid duplicating the startup sequence.
func (s *Server) initAndListen(ctx context.Context) (net.Listener, error) {
	mux := s.buildMux()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}
	s.startedAt = time.Now()
	s.startTickBroadcaster(ctx)
	s.StartMonitoring(ctx)
	s.startProcessPruner(ctx)
	s.sessions.StartGC(ctx)

	// Mark ready after all background subsystems have started.
	s.ready.Store(true)

	// Auto-start all registered channel plugins.
	if s.channelLifecycle != nil {
		s.safeGo("channels:start-all", func() {
			if errs := s.channelLifecycle.StartAll(ctx); len(errs) > 0 {
				for id, err := range errs {
					s.logger.Warn("channel auto-start failed", "channel", id, "error", err)
				}
			}
		})
	}

	// Start autonomous service (dreaming lifecycle).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Start()
	}

	// Start Gmail polling service.
	if s.gmailPollSvc != nil {
		s.safeGo("gmailpoll:start", func() {
			s.gmailPollSvc.Start(ctx)
		})
	}

	// Fire gateway.start hooks.
	if s.hooks != nil {
		addr := ln.Addr().String()
		s.safeGo("hooks:gateway.start", func() {
			s.hooks.Fire(context.Background(), hooks.EventGatewayStart, map[string]string{
				"DENEB_GATEWAY_ADDR": addr,
			})
		})
	}

	return ln, nil
}

// Run starts the server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := s.initAndListen(ctx)
	if err != nil {
		return err
	}

	s.logger.Info("gateway server starting", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

// StartAndListen starts the server and returns its actual address (useful with port ":0").
// The caller must call Close() to stop the server; the serve goroutine is tied to
// the http.Server lifecycle and will exit when Shutdown is called.
func (s *Server) StartAndListen(ctx context.Context) (net.Addr, error) {
	ln, err := s.initAndListen(ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve error", "error", err)
		}
	}()

	return ln.Addr(), nil
}

// Close gracefully shuts down the server.
func (s *Server) Close(ctx context.Context) error {
	return s.shutdown()
}

func (s *Server) shutdown() error {
	var httpErr error
	s.shutdownOnce.Do(func() {
		httpErr = s.doShutdown()
	})
	return httpErr
}

func (s *Server) doShutdown() error {
	s.ready.Store(false)
	logging.PrintShutdown(os.Stderr, time.Since(s.startedAt), s.logColor)

	// 1. Broadcast shutdown event to all connected clients.
	s.broadcastShutdownEvent()

	// 2. Stop accepting new connections.
	var httpErr error
	if s.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpErr = s.httpServer.Shutdown(shutdownCtx)
	}

	// 3. Close existing WebSocket clients.
	s.clients.Range(func(key, value any) bool {
		client := value.(*WsClient)
		if err := client.conn.Close(websocket.StatusGoingAway, "server shutting down"); err != nil {
			s.logger.Debug("ws close during shutdown", "connId", client.connID, "error", err)
		}
		return true
	})

	// 4. Stop gateway event subscriptions (bounded to avoid hanging).
	if s.gatewaySubs != nil {
		done := make(chan struct{})
		go func() {
			s.gatewaySubs.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			s.logger.Warn("gatewaySubs.Stop timed out after 5s")
		}
	}

	// 5. Stop dedupe background GC.
	s.dedupe.Close()

	// 6. Stop cron scheduler.
	if s.cron != nil {
		s.cron.Close()
	}

	// 6b. Stop autonomous service (dreaming).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Stop()
	}

	// 6c. Stop Gmail poll service.
	if s.gmailPollSvc != nil {
		s.gmailPollSvc.Stop()
	}

	// 7. Fire gateway.stop hooks.
	if s.hooks != nil {
		s.hooks.Fire(context.Background(), hooks.EventGatewayStop, nil)
	}

	// 8. Stop all channel plugins.
	if s.channelLifecycle != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.channelLifecycle.StopAll(stopCtx)
		stopCancel()
	}

	// 9. Close chat handler.
	if s.chatHandler != nil {
		s.chatHandler.Close()
	}

	// 10. Close auth rate limiter.
	if s.authRateLimiter != nil {
		s.authRateLimiter.Close()
	}

	// 11. Close Vega backend.
	if s.vegaBackend != nil {
		s.vegaBackend.Close()
	}

	// 12. ACP cleanup: persist bindings and unsubscribe lifecycle sync.
	if s.acpDeps != nil && s.acpDeps.BindingStore != nil && s.acpDeps.Bindings != nil {
		if err := s.acpDeps.BindingStore.SyncFromService(s.acpDeps.Bindings); err != nil {
			s.logger.Warn("failed to persist ACP bindings on shutdown", "error", err)
		}
	}
	if s.acpLifecycleUnsub != nil {
		s.acpLifecycleUnsub()
	}

	return httpErr
}

// broadcastShutdownEvent sends a shutdown event to all authenticated clients
// so they can reconnect or show an appropriate message.
func (s *Server) broadcastShutdownEvent() {
	ev, _ := protocol.NewEventFrame("shutdown", map[string]any{
		"reason": "server shutting down",
	})
	s.clients.Range(func(key, value any) bool {
		client := value.(*WsClient)
		if client.authed {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			s.writeFrame(ctx, client, ev)
			cancel()
		}
		return true
	})
}
