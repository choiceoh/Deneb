// Package server implements the HTTP + WebSocket gateway server.
//
// Handles health endpoints, WebSocket connections with the full handshake
// protocol, RPC dispatch, OpenAI-compatible HTTP APIs, hooks webhooks,
// session management, and plugin HTTP routing.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/copilot"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
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
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
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

	// Phase 5: Autonomous goal-driven execution.
	autonomousSvc   *autonomous.Service
	dreamingAdapter *memory.DreamingAdapter // stored in phase 2, wired to autonomous in phase 3

	// toolDeps holds core tool dependencies; stored on the server so late-binding
	// fields (e.g. AutonomousSvc) can be set from other init phases.
	toolDeps *chat.CoreToolDeps

	// Copilot: background system monitor using local sglang.
	copilotSvc *copilot.Service

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

	// Start autonomous service (Phase 2 attention timer).
	if s.autonomousSvc != nil {
		s.safeGo("autonomous:start", func() {
			s.autonomousSvc.Start(ctx, autonomous.DefaultAttentionConfig())
		})
	}

	// Start copilot background system monitor.
	if s.copilotSvc != nil {
		s.safeGo("copilot:start", func() {
			s.copilotSvc.Start(ctx)
		})
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

	// 6b. Stop autonomous service.
	if s.autonomousSvc != nil {
		s.autonomousSvc.Stop()
	}

	// 6c. Stop copilot service.
	if s.copilotSvc != nil {
		s.copilotSvc.Stop()
	}

	// 6d. Stop Gmail poll service.
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

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("POST /api/v1/rpc", s.handleRPC)
	mux.HandleFunc("GET /ws", s.handleWsUpgrade)

	// HTTP API endpoints (P2 migration).
	mux.HandleFunc("POST /tools/invoke", s.handleToolsInvoke)
	mux.HandleFunc("POST /sessions/{key}/kill", s.handleSessionKill)
	mux.HandleFunc("GET /sessions/{key}/history", s.handleSessionHistory)

	// OpenAI-compatible HTTP API endpoints.
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/responses", s.handleResponses)

	// Hooks HTTP webhook endpoint — intercepts /hooks/* before the fallback.
	if s.hooksHTTP != nil {
		hooksHandler := s.hooksHTTP
		mux.HandleFunc("/hooks/", func(w http.ResponseWriter, r *http.Request) {
			if !hooksHandler.Handle(w, r) {
				http.NotFound(w, r)
			}
		})
		mux.HandleFunc("/hooks", func(w http.ResponseWriter, r *http.Request) {
			if !hooksHandler.Handle(w, r) {
				http.NotFound(w, r)
			}
		})
	}

	// Catch-all handler: plugin HTTP routes → root fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Plugin HTTP routes.
		if s.pluginRouter != nil && s.pluginRouter.Handle(w, r) {
			return
		}
		// Root fallback for exact "/" GET.
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			s.handleRoot(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

// handleMetrics responds with Prometheus-compatible text exposition of all metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.WriteMetrics(w)
}

// handleHealth responds with gateway health status including subsystem state.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	authMode := ""
	providerCount := 0
	if s.runtimeCfg != nil {
		authMode = s.runtimeCfg.AuthMode
	}
	if s.providers != nil {
		providerCount = len(s.providers.List())
	}

	// Count active processes.
	activeProcesses := 0
	if s.processes != nil {
		for _, p := range s.processes.List() {
			if p.Status == process.StatusRunning {
				activeProcesses++
			}
		}
	}

	// Count cron tasks.
	cronTasks := 0
	if s.cron != nil {
		cronTasks = len(s.cron.List())
	}

	// Count registered hooks.
	hooksCount := 0
	if s.hooks != nil {
		hooksCount = len(s.hooks.List())
	}

	// Channel health summary.
	channelHealthSummary := map[string]int{"healthy": 0, "unhealthy": 0}
	if s.channelHealth != nil {
		for _, ch := range s.channelHealth.HealthSnapshot() {
			if ch.Healthy {
				channelHealthSummary["healthy"]++
			} else {
				channelHealthSummary["unhealthy"]++
			}
		}
	}

	uptime := time.Since(s.startedAt)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"version":   s.version,
		"uptime":    formatUptimeHTTP(uptime),
		"uptime_ms": uptime.Milliseconds(),
		"subsystems": map[string]any{
			"core": coreLabel(s.rustFFI),
			"vega": s.vegaBackend != nil,
			"auth": authMode,
		},
		"connections": s.clientCnt.Load(),
		"sessions":    s.sessions.Count(),
		"channels":    channelHealthSummary,
		"workers": map[string]int{
			"processes": activeProcesses,
			"cron":      cronTasks,
			"hooks":     hooksCount,
		},
		"providers": providerCount,
	})
}

// handleReady responds with readiness status.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	ready := s.ready.Load()
	httpStatus := http.StatusOK
	statusLabel := "ok"
	if !ready {
		httpStatus = http.StatusServiceUnavailable
		statusLabel = "unavailable"
	}
	s.writeJSON(w, httpStatus, map[string]any{
		"status": statusLabel,
		"ready":  ready,
	})
}

// writeJSON encodes v as JSON to the response writer, logging any encoding errors.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("json encode error", "error", err)
	}
}

// coreLabel returns a human-readable label for the core backend.
func coreLabel(rustFFI bool) string {
	if rustFFI {
		return "rust-ffi"
	}
	return "go"
}

// formatUptimeHTTP returns a human-readable uptime string for HTTP responses.
func formatUptimeHTTP(d time.Duration) string {
	d = d.Round(time.Second)
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	rs := s % 60
	if m < 60 {
		if rs == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, rs)
	}
	h := m / 60
	rm := m % 60
	if rm == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, rm)
}

// handleRPC processes HTTP JSON-RPC requests via the dispatcher.
// Extracts Bearer token from Authorization header for authentication.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// Track activity.
	if s.activity != nil {
		s.activity.Touch()
	}

	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
		ID     string          `json:"id"`
	}

	limited := http.MaxBytesReader(w, r.Body, maxRPCBodyBytes)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, protocol.NewResponseError("", protocol.NewError(
			protocol.ErrInvalidRequest, "invalid JSON",
		)))
		return
	}

	if req.Method == "" || req.ID == "" {
		s.writeJSON(w, http.StatusBadRequest, protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "method and id are required",
		)))
		return
	}

	// Resolve auth from Bearer token.
	role := ""
	authenticated := false
	var scopes []auth.Scope

	if s.authValidator != nil {
		token := extractBearerToken(r)
		if token != "" {
			claims, err := s.authValidator.ValidateToken(token)
			if err != nil {
				s.writeJSON(w, http.StatusUnauthorized, protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnauthorized, "invalid token: "+err.Error(),
				)))
				return
			}
			role = string(claims.Role)
			authenticated = true
			scopes = claims.Scopes
		}
	} else {
		// No-auth mode: treat all HTTP requests as operator.
		role = "operator"
		authenticated = true
		scopes = auth.DefaultScopes(auth.RoleOperator)
	}

	// Authorize method call.
	if authErr := rpc.AuthorizeMethod(req.Method, role, authenticated, scopes); authErr != nil {
		status := http.StatusForbidden
		if authErr.Code == protocol.ErrUnauthorized {
			status = http.StatusUnauthorized
		}
		s.writeJSON(w, status, protocol.NewResponseError(req.ID, authErr))
		return
	}

	frame := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     req.ID,
		Method: req.Method,
		Params: req.Params,
	}

	dispatchCtx, dispatchCancel := context.WithTimeout(r.Context(), timeouts.RPCDispatch)
	resp := s.dispatcher.Dispatch(dispatchCtx, frame)
	dispatchCancel()

	s.writeJSON(w, http.StatusOK, resp)
}

// extractBearerToken extracts the token from an "Authorization: Bearer <token>" header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return authHeader[len(prefix):]
	}
	return ""
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":    "deneb-gateway",
		"version": s.version,
		"status":  "ok",
	})
}

// registerExtendedMethods registers Phase 2 RPC methods (process, cron, hooks, agent, ACP).
func (s *Server) registerExtendedMethods() {
	// ACP RPC methods.
	rpc.RegisterACPMethods(s.dispatcher, s.acpDeps)

	rpc.RegisterExtendedMethods(s.dispatcher, rpc.ExtendedDeps{
		Sessions:    s.sessions,
		Channels:    s.channels,
		GatewaySubs: s.gatewaySubs,
		Processes:   s.processes,
		Cron:        s.cron,
		Hooks:       s.hooks,
		Broadcaster: s.broadcaster,
	})

	// Provider methods.
	rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
		Providers:   s.providers,
		AuthManager: s.authManager,
	})

	// Tool methods.
	rpc.RegisterToolMethods(s.dispatcher, rpc.ToolDeps{
		Processes: s.processes,
	})

	// Session state methods (patch/reset/preview/resolve/compact).
	var sessionCompressor *transcript.Compressor
	if s.transcript != nil {
		sessionCompressor = transcript.NewCompressor(transcript.DefaultCompactionConfig(), s.logger)
	}
	sessionDeps := rpc.SessionDeps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
		Transcripts: s.transcript,
		Compressor:  sessionCompressor,
	}
	rpc.RegisterSessionMethods(s.dispatcher, sessionDeps)

	// Session repair and overflow check methods.
	rpc.RegisterSessionRepairMethods(s.dispatcher, sessionDeps)

	// Daemon status method.
	s.dispatcher.Register("daemon.status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.daemon == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]string{"state": "not_configured"})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, s.daemon.Status())
		return resp
	})

	// Aurora channel methods (desktop app communication).
	rpc.RegisterAuroraChannelMethods(s.dispatcher, rpc.AuroraChannelDeps{
		Chat: s.chatHandler,
	})

	// Event broadcasting method.
	s.dispatcher.Register("events.broadcast", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})
}

// SetDaemon sets the daemon manager for lifecycle control.
func (s *Server) SetDaemon(d *daemon.Daemon) {
	s.daemon = d
}

// SetVega sets the Vega backend and registers its RPC methods.
func (s *Server) SetVega(backend vega.Backend) {
	s.vegaBackend = backend
	rpc.RegisterVegaMethods(s.dispatcher, rpc.VegaDeps{Backend: backend})
	// Late-bind Vega backend into core tool deps so the vega chat tool works.
	if s.toolDeps != nil {
		s.toolDeps.VegaBackend = backend
	}
}


// Broadcaster returns the event broadcaster for external use.
func (s *Server) Broadcaster() *events.Broadcaster {
	return s.broadcaster
}

// GatewaySubscriptions returns the gateway event subscription manager
// for emitting agent, heartbeat, transcript, and lifecycle events.
func (s *Server) GatewaySubscriptions() *events.GatewayEventSubscriptions {
	return s.gatewaySubs
}

// registerPhase2Methods registers chat, config, monitoring, and event subscription methods.
func (s *Server) registerPhase2Methods() {
	// Chat methods — native agent execution.
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}

	// Determine transcript base directory.
	transcriptDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		transcriptDir = home + "/.deneb/transcripts"
	}
	var transcriptStore chat.TranscriptStore
	if transcriptDir != "" {
		transcriptStore = chat.NewCachedTranscriptStore(
			chat.NewFileTranscriptStore(transcriptDir), 0)
	}

	// Initialize agent detail log writer.
	var agentLogWriter *agentlog.Writer
	if home, err := os.UserHomeDir(); err == nil {
		agentLogWriter = agentlog.NewWriter(home + "/.deneb/agent-logs")
		s.logger.Info("agent detail log initialized", "dir", home+"/.deneb/agent-logs")
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter

	// Initialize Aurora compaction store.
	auroraStore, err := aurora.NewStore(aurora.DefaultStoreConfig(), s.logger)
	if err != nil {
		s.logger.Warn("aurora store unavailable, compaction will use legacy fallback", "error", err)
	} else {
		chatCfg.AuroraStore = auroraStore
		s.logger.Info("aurora compaction store initialized")
	}

	// Initialize structured memory store (Honcho-style).
	if home, err := os.UserHomeDir(); err == nil {
		dbPath := filepath.Join(home, ".deneb", "memory.db")
		memStore, err := memory.NewStore(dbPath)
		if err != nil {
			s.logger.Warn("memory store unavailable", "error", err)
		} else {
			chatCfg.MemoryStore = memStore

			const sglangURL = "http://127.0.0.1:30000/v1"
			const sglangModel = "Qwen/Qwen3.5-35B-A3B"

			// Use the Gemini embedder set during gateway init.
			if s.geminiEmbedder != nil {
				embedder := memory.NewEmbedder(s.geminiEmbedder, memStore, s.logger)
				chatCfg.MemoryEmbedder = embedder

				sglangClient := llm.NewClient(sglangURL, "", llm.WithLogger(s.logger))
				s.dreamingAdapter = memory.NewDreamingAdapter(memStore, embedder, sglangClient, sglangModel, s.logger)
				// DreamTurnFn is wired after autonomous service is created (phase 3).
				// Use a closure that captures s so the autonomous svc reference resolves at call time.
				chatCfg.DreamTurnFn = func(ctx context.Context) {
					if svc := s.autonomousSvc; svc != nil {
						svc.IncrementDreamTurn(ctx)
					}
				}
			} else {
				s.logger.Info("aurora-memory: embedding disabled (GEMINI_API_KEY not set)")
			}

			// Wire cross-encoder reranker if Jina API key is configured.
			if s.jinaAPIKey != "" {
				reranker := vega.NewReranker(vega.RerankConfig{
					APIKey: s.jinaAPIKey,
					Logger: s.logger,
				})
				if reranker != nil {
					memStore.SetReranker(func(ctx context.Context, query string, docs []string, topN int) ([]memory.RerankResult, error) {
						vegaResults, err := reranker.Rerank(ctx, query, docs, topN)
						if err != nil {
							return nil, err
						}
						results := make([]memory.RerankResult, len(vegaResults))
						for i, r := range vegaResults {
							results[i] = memory.RerankResult{Index: r.Index, RelevanceScore: r.RelevanceScore}
						}
						return results, nil
					})
					s.logger.Info("aurora-memory: cross-encoder reranking enabled (Jina)")
				}
			}

			// Auto-migrate existing MEMORY.md on first run.
			count, _ := memStore.ActiveFactCount(context.Background())
			if count == 0 {
				memoryMdPath := filepath.Join(home, ".deneb", "MEMORY.md")
				if imported, err := memStore.ImportFromMarkdown(context.Background(), memoryMdPath); err == nil && imported > 0 {
					s.logger.Info("aurora-memory: imported legacy MEMORY.md", "facts", imported)
				}
			}

			s.logger.Info("aurora-memory: structured store initialized", "db", dbPath)
		}
	}

	// Resolve default model from config; fall back to hardcoded default.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)

	// Resolve workspace directory for file tool operations.
	workspaceDir := resolveWorkspaceDir()
	s.logger.Info("resolved agent workspace directory", "workspaceDir", workspaceDir)

	// Build core tool dependencies. Stored on the server so later init phases
	// (e.g. registerAdvancedWorkflowMethods) can late-bind fields like AutonomousSvc.
	s.toolDeps = &chat.CoreToolDeps{
		ProcessMgr:   s.processes,
		WorkspaceDir: workspaceDir,
		CronSched:    s.cron,
		Sessions:     s.sessions,
		LLMClient:    chatCfg.LLMClient,
		Transcript:   transcriptStore,
		AgentLog:     agentLogWriter,
		MemoryStore:  chatCfg.MemoryStore,
	}

	// Register core tools (file I/O, exec, process, sessions, gateway, cron, image).
	chat.RegisterCoreTools(chatCfg.Tools, s.toolDeps)
	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = loadProviderConfigs(s.logger)

	s.chatHandler = chat.NewHandler(
		s.sessions,
		broadcastFn,
		s.logger,
		chatCfg,
	)

	// Wire SessionSendFn after handler creation to avoid circular deps.
	s.toolDeps.SessionSendFn = func(sessionKey, message string) error {
		fakeReq := &protocol.RequestFrame{
			ID:     shortid.New("tool_send"),
			Method: "sessions.send",
		}
		params := map[string]string{"key": sessionKey, "message": message}
		fakeReq.Params, _ = json.Marshal(params)
		resp := s.chatHandler.SessionsSend(context.Background(), fakeReq)
		if resp != nil && resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		return nil
	}
	rpc.RegisterChatMethods(s.dispatcher, rpc.ChatDeps{Chat: s.chatHandler})

	// Wire raw broadcast directly to chat handler for streaming event relay.
	s.chatHandler.SetBroadcastRaw(func(event string, data []byte) int {
		return s.broadcaster.BroadcastRaw(event, data)
	})

	// Side-question (/btw) method — routes through chat handler natively.
	rpc.RegisterChatBtwMethods(s.dispatcher, rpc.ChatBtwDeps{
		Chat:        s.chatHandler,
		Broadcaster: broadcastFn,
	})

	// Native session execution / agent methods (Phase 4).
	rpc.RegisterSessionExecMethods(s.dispatcher, rpc.SessionExecDeps{
		Chat:       s.chatHandler,
		Agents:     s.agents,
		JobTracker: s.jobTracker,
	})

	// Config reload method with Go subsystem propagation.
	rpc.RegisterConfigReloadMethod(s.dispatcher, rpc.ConfigReloadDeps{
		OnReloaded: func(_ *config.ConfigSnapshot) {
			// Notify hooks of config change.
			if s.hooks != nil {
				s.safeGo("hooks:config.reloaded", func() {
					s.hooks.Fire(context.Background(), hooks.Event("config.reloaded"), nil)
				})
			}
			// Broadcast config change to subscribers.
			s.broadcaster.Broadcast("config.changed", map[string]any{
				"ts": time.Now().UnixMilli(),
			})
			// Restart channels to pick up config changes.
			if s.channelLifecycle != nil {
				s.safeGo("config:restart-channels", func() {
					reloadCtx := context.Background()
					if errs := s.channelLifecycle.StopAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel stop failed", "channel", id, "error", err)
						}
					}
					if errs := s.channelLifecycle.StartAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel start failed", "channel", id, "error", err)
						}
					}
					s.logger.Info("config reload: channels restarted")
				})
			}
			// Restart cron scheduler.
			if s.cron != nil {
				s.safeGo("config:restart-cron", func() {
					s.cron.Close()
					s.cron = cron.NewScheduler(s.logger)
					s.logger.Info("config reload: cron scheduler restarted")
				})
			}
		},
	})

	// Monitoring methods.
	rpc.RegisterMonitoringMethods(s.dispatcher, rpc.MonitoringDeps{
		ChannelHealth: s.channelHealth,
		Activity:      s.activity,
	})

	// Channel lifecycle RPC methods.
	rpc.RegisterChannelLifecycleMethods(s.dispatcher, rpc.ChannelLifecycleDeps{
		ChannelLifecycle: s.channelLifecycle,
		Hooks:            s.hooks,
		Broadcaster:      s.broadcaster,
	})

	// Event subscription methods.
	rpc.RegisterEventsMethods(s.dispatcher, rpc.EventsDeps{Broadcaster: s.broadcaster, Logger: s.logger})

	// Gateway identity method.
	rpc.RegisterIdentityMethods(s.dispatcher, s.version)

	// Heartbeat methods (last-heartbeat, set-heartbeats).
	if s.heartbeatState == nil {
		s.heartbeatState = rpc.NewHeartbeatState()
	}
	rpc.RegisterHeartbeatMethods(s.dispatcher, rpc.HeartbeatDeps{
		State:       s.heartbeatState,
		Broadcaster: broadcastFn,
	})

	// System presence methods (system-presence, system-event).
	if s.presenceStore == nil {
		s.presenceStore = rpc.NewPresenceStore()
	}
	rpc.RegisterPresenceMethods(s.dispatcher, rpc.PresenceDeps{
		Store:       s.presenceStore,
		Broadcaster: broadcastFn,
	})

	// Models list method.
	rpc.RegisterModelsMethods(s.dispatcher, rpc.ModelsDeps{
		Providers: s.providers,
	})

	// Stub handlers for methods that required the removed Node.js bridge.
	// Registered explicitly so callers receive ErrUnavailable instead of
	// "unknown method", and RPC parity tests pass.
	stubUnavailable := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, req.Method+" not available (requires browser/web-login integration)"))
	}
	s.dispatcher.Register("browser.request", stubUnavailable)
	s.dispatcher.Register("web.login.start", stubUnavailable)
	s.dispatcher.Register("web.login.wait", stubUnavailable)
	s.dispatcher.Register("channels.logout", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Channel string `json:"channel"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Channel == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "channel is required"))
		}
		// Validate channel exists.
		ch := s.channels.Get(p.Channel)
		if ch == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "channel not found: "+p.Channel))
		}
		// Stop the channel (logout = stop + clear).
		loggedOut := true
		if s.channelLifecycle != nil {
			if err := s.channelLifecycle.StopChannel(ctx, p.Channel); err != nil {
				s.logger.Warn("channels.logout: stop failed", "channel", p.Channel, "error", err)
				loggedOut = false
			}
		}
		// Broadcast channel change event.
		if loggedOut {
			s.broadcaster.Broadcast("channels.changed", map[string]any{
				"channelId": p.Channel,
				"action":    "logged_out",
				"ts":        time.Now().UnixMilli(),
			})
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":        true,
			"channel":   p.Channel,
			"loggedOut": loggedOut,
			"cleared":   loggedOut,
		})
		return resp
	})
}

// registerAdvancedWorkflowMethods registers Phase 3 RPC methods for exec approvals,
// nodes, devices, agents, cron advanced, config advanced, skills, wizard, secrets, and talk.
func (s *Server) registerAdvancedWorkflowMethods() {
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}

	rpc.RegisterApprovalMethods(s.dispatcher, rpc.ApprovalDeps{
		Store:       s.approvals,
		Broadcaster: broadcastFn,
	})

	// Wire process approval callback using the Go approval store directly.
	// When a tool execution requires approval, create an approval request,
	// broadcast it to WS clients, and wait for a decision.
	if s.processes != nil {
		s.processes.SetApprover(func(req process.ExecRequest) bool {
			ar := s.approvals.CreateRequest(approval.CreateRequestParams{
				Command:     req.Command,
				CommandArgv: req.Args,
				Cwd:         req.WorkingDir,
			})
			broadcastFn("exec.approval.requested", map[string]any{
				"id":      ar.ID,
				"command": req.Command,
				"args":    req.Args,
			})
			// Wait for decision with timeout.
			waitCh := s.approvals.WaitForDecision(ar.ID)
			timer := time.NewTimer(30 * time.Second)
			defer timer.Stop()
			select {
			case <-waitCh:
				resolved := s.approvals.Get(ar.ID)
				if resolved != nil && resolved.Decision != nil {
					return *resolved.Decision == approval.DecisionAllowOnce || *resolved.Decision == approval.DecisionAllowAlways
				}
				return false
			case <-timer.C:
				return false
			}
		})
	}

	canvasHost := ""
	if s.runtimeCfg != nil {
		canvasHost = fmt.Sprintf("http://%s:%d", s.runtimeCfg.BindHost, s.runtimeCfg.Port)
	}
	rpc.RegisterNodeMethods(s.dispatcher, rpc.NodeDeps{
		Nodes:       s.nodes,
		Broadcaster: broadcastFn,
		CanvasHost:  canvasHost,
	})

	rpc.RegisterDeviceMethods(s.dispatcher, rpc.DeviceDeps{
		Devices:     s.devices,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterCronAdvancedMethods(s.dispatcher, rpc.CronAdvancedDeps{
		Cron:        s.cron,
		RunLog:      s.cronRunLog,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterAgentsMethods(s.dispatcher, rpc.AgentsDeps{
		Agents:      s.agents,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterConfigAdvancedMethods(s.dispatcher, rpc.ConfigAdvancedDeps{
		Broadcaster: broadcastFn,
	})

	rpc.RegisterSkillMethods(s.dispatcher, rpc.SkillDeps{
		Skills:      s.skills,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterWizardMethods(s.dispatcher, rpc.WizardDeps{
		Engine: s.wizardEng,
	})

	rpc.RegisterSecretMethods(s.dispatcher, rpc.SecretDeps{
		Resolver: s.secrets,
	})

	rpc.RegisterTalkMethods(s.dispatcher, rpc.TalkDeps{
		Talk: s.talkState,
	})

	// Autonomous goal-driven execution.
	homeDir, _ := os.UserHomeDir()
	s.autonomousSvc = autonomous.NewService(autonomous.ServiceConfig{
		GoalStorePath: autonomous.DefaultGoalStorePath(homeDir),
	}, &autonomousAgentAdapter{
		chatHandler: s.chatHandler,
		jobTracker:  s.jobTracker,
		transcript:  s.transcript,
		sessions:    s.sessions,
	}, s.logger)

	// Wire autonomous service into agent tools (late-binding, same as SessionSendFn).
	s.toolDeps.AutonomousSvc = s.autonomousSvc


	// Wire AuroraDream adapter if available (created in phase 2).
	if s.dreamingAdapter != nil {
		s.autonomousSvc.SetDreamer(s.dreamingAdapter)
	}

	// Broadcast autonomous cycle events (including dreaming) to WebSocket clients.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		if strings.HasPrefix(event.Type, "dreaming_") {
			broadcastFn("dreaming.cycle", event)
		} else {
			broadcastFn("autonomous.cycle", event)
		}
	})

	// Wire Telegram notifier for significant autonomous events (goal completion,
	// auto-pause, consecutive errors). Single-user deployment: use the first
	// allowed user ID as the DM chat ID.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.autonomousSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
	}

	rpc.RegisterAutonomousMethods(s.dispatcher, rpc.AutonomousDeps{
		Autonomous: s.autonomousSvc,
	})

	// Wire autonomous wake dispatcher into hooks HTTP handler.
	if s.hooksHTTP != nil {
		s.hooksHTTP.SetAutonomousWakeDispatcher(func(text string) {
			if s.autonomousSvc != nil && s.autonomousSvc.Goals() != nil {
				s.safeGo("autonomous:external-wake", func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
					defer cancel()
					s.autonomousSvc.RunCycle(ctx)
				})
			}
		})
	}

	// Copilot: background system monitor using local sglang.
	s.copilotSvc = copilot.NewService(copilot.ServiceConfig{
		CheckIntervalMin: 15,
		SglangBaseURL:    "http://127.0.0.1:30000/v1",
		SglangModel:      "Qwen/Qwen3.5-35B-A3B",
	}, s.logger)

	// Wire Telegram notifier for copilot alerts.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.copilotSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
	}

	rpc.RegisterCopilotMethods(s.dispatcher, rpc.CopilotDeps{
		Copilot: s.copilotSvc,
	})

	// Gmail polling service: periodic new-email analysis via LLM.
	s.initGmailPoll()
}

// initGmailPoll initializes the Gmail polling service if enabled in config.
func (s *Server) initGmailPoll() {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".deneb")

	cfg := gmailpoll.Config{
		StateDir:   stateDir,
		LLMBaseURL: "http://127.0.0.1:30000/v1",
	}
	if pollCfg.IntervalMin != nil {
		cfg.IntervalMin = *pollCfg.IntervalMin
	}
	if pollCfg.Query != "" {
		cfg.Query = pollCfg.Query
	}
	if pollCfg.MaxPerCycle != nil {
		cfg.MaxPerCycle = *pollCfg.MaxPerCycle
	}
	if pollCfg.Model != "" {
		cfg.Model = pollCfg.Model
	}
	if pollCfg.PromptFile != "" {
		cfg.PromptFile = pollCfg.PromptFile
	}

	s.gmailPollSvc = gmailpoll.NewService(cfg, s.logger)

	// Wire Telegram notifier.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.gmailPollSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
	}

	s.logger.Info("gmailpoll service initialized")
}

// registerNativeSystemMethods registers native Go system RPC methods:
// usage, logs, doctor, maintenance, update.
func (s *Server) registerNativeSystemMethods(denebDir string) {
	rpc.RegisterUsageMethods(s.dispatcher, rpc.UsageDeps{
		Tracker: s.usageTracker,
	})

	rpc.RegisterLogsMethods(s.dispatcher, rpc.LogsDeps{
		LogDir: filepath.Join(denebDir, "logs"),
	})

	rpc.RegisterDoctorMethods(s.dispatcher, rpc.DoctorDeps{})

	rpc.RegisterMaintenanceMethods(s.dispatcher, rpc.MaintenanceDeps{
		Runner: s.maintRunner,
	})

	rpc.RegisterUpdateMethods(s.dispatcher, rpc.UpdateDeps{
		DenebDir: denebDir,
	})

	// Telegram native channel plugin + messaging methods.
	// Loads Telegram config from deneb.json if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		if tgCfg != nil && tgCfg.BotToken != "" {
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			s.channels.Register(s.telegramPlug)
		}
	}
	rpc.RegisterMessagingMethods(s.dispatcher, rpc.MessagingDeps{
		TelegramPlugin: s.telegramPlug,
	})

	// Wire Telegram update handler → autoreply preprocessing → chat.send pipeline.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

}

// wireTelegramChatHandler connects the Telegram polling handler to the chat
// handler via the autoreply inbound processor so incoming messages go through
// command detection, directive parsing, and normalization before reaching the
// LLM agent.
func (s *Server) wireTelegramChatHandler() {
	// Recent-send dedup cache: prevents the same text from being delivered
	// to the same chat twice within a short window (e.g. when the LLM uses
	// the message tool AND also produces a text response without NO_REPLY).
	var recentMu sync.Mutex
	recentSends := make(map[string]time.Time) // key: "chatID:text[:200]"
	const recentTTL = 10 * time.Second

	// Set reply function: delivers assistant responses back to Telegram.
	s.chatHandler.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
		if delivery == nil || delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := strconv.ParseInt(delivery.To, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		// Dedup: skip if the same text was sent to this chat recently.
		dedupKey := delivery.To + ":" + truncateForDedup(text, 200)
		recentMu.Lock()
		if sentAt, dup := recentSends[dedupKey]; dup && time.Since(sentAt) < recentTTL {
			recentMu.Unlock()
			s.logger.Info("suppressed duplicate reply to telegram",
				"chatId", delivery.To, "textLen", len(text))
			return nil
		}
		// Evict stale entries (cheap, single-user so map stays tiny).
		for k, t := range recentSends {
			if time.Since(t) >= recentTTL {
				delete(recentSends, k)
			}
		}
		recentSends[dedupKey] = time.Now()
		recentMu.Unlock()

		// Parse optional button directive from agent reply.
		cleanText, keyboard := parseReplyButtons(text)
		opts := telegram.SendOptions{ParseMode: "HTML", Keyboard: keyboard}
		html := telegram.MarkdownToTelegramHTML(cleanText)
		_, err = telegram.SendText(ctx, client, chatID, html, opts)
		return err
	})

	// Set media send function: delivers files back to Telegram.
	s.chatHandler.SetMediaSendFunc(func(ctx context.Context, delivery *chat.DeliveryContext, filePath, mediaType, caption string, silent bool) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := strconv.ParseInt(delivery.To, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		fileName := filepath.Base(filePath)
		opts := telegram.SendOptions{DisableNotification: silent}

		switch mediaType {
		case "photo":
			_, err = telegram.UploadPhoto(ctx, client, chatID, fileName, f, caption, opts)
		case "video":
			// Upload as document — Telegram sendVideo requires a URL/file_id, not multipart.
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "audio":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "voice":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		default: // "document" or unknown
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		}
		return err
	})

	// Set typing indicator function: sends "typing" chat action to Telegram
	// periodically during agent runs so the user sees "typing..." in the chat.
	s.chatHandler.SetTypingFunc(func(ctx context.Context, delivery *chat.DeliveryContext) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := strconv.ParseInt(delivery.To, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		return client.SendChatAction(ctx, chatID, "typing")
	})

	// Set reaction function: sets emoji reactions on the user's triggering message
	// to show agent status phases (👀→🤔→🔥→👍).
	s.chatHandler.SetReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
		if delivery == nil || delivery.Channel != "telegram" || delivery.MessageID == "" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := strconv.ParseInt(delivery.To, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		msgID, err := strconv.ParseInt(delivery.MessageID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid message ID %q: %w", delivery.MessageID, err)
		}
		return client.SetMessageReaction(ctx, chatID, msgID, emoji)
	})

	// Create the inbound processor that routes Telegram messages through
	// the autoreply command/directive pipeline before dispatching to chat.send.
	inbound := NewInboundProcessor(s)

	// Set update handler: routes through autoreply preprocessing → chat.send.
	s.telegramPlug.SetHandler(func(_ context.Context, update *telegram.Update) {
		inbound.HandleTelegramUpdate(update)
	})

	s.logger.Info("telegram chat handler wired (with autoreply preprocessing)")
}

// loadTelegramConfig extracts Telegram channel config from deneb.json.
// Returns nil if Telegram is not configured.
func loadTelegramConfig(_ *config.GatewayRuntimeConfig) *telegram.Config {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid {
		return nil
	}

	// Extract channels.telegram from raw config JSON.
	if snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Channels struct {
			Telegram *telegram.Config `json:"telegram"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return nil
	}
	return root.Channels.Telegram
}



// loadProviderConfigs reads LLM provider configs (apiKey, baseUrl, api) from deneb.json.
func loadProviderConfigs(logger *slog.Logger) map[string]chat.ProviderConfig {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Models struct {
			Providers map[string]chat.ProviderConfig `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse provider configs", "error", err)
		return nil
	}

	if len(root.Models.Providers) > 0 {
		logger.Info("loaded provider configs", "count", len(root.Models.Providers))
	}
	return root.Models.Providers
}

// resolveDefaultModel reads agents.defaultModel or agents.defaults.model from
// deneb.json, falling back to a hardcoded default.
// The model field can be either a string ("model-name") or an object
// ({"primary": "model-name", "fallbacks": [...]}).
func resolveDefaultModel(logger *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return "google/gemini-3.0-flash"
	}
	var root struct {
		Agents struct {
			DefaultModel string          `json:"defaultModel"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for model", "error", err)
		return "google/gemini-3.0-flash"
	}
	if root.Agents.DefaultModel != "" {
		return root.Agents.DefaultModel
	}
	if len(root.Agents.Defaults) > 0 {
		model := extractModelFromDefaults(root.Agents.Defaults)
		if model != "" {
			return model
		}
	}
	return "google/gemini-3.0-flash"
}

// extractModelFromDefaults handles both string and object forms of the model field.
func extractModelFromDefaults(raw json.RawMessage) string {
	var defaults struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(raw, &defaults); err != nil || len(defaults.Model) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(defaults.Model, &s); err == nil && s != "" {
		return s
	}
	// Try object with primary field.
	var obj struct {
		Primary string `json:"primary"`
	}
	if err := json.Unmarshal(defaults.Model, &obj); err == nil && obj.Primary != "" {
		return obj.Primary
	}
	return ""
}

// resolveWorkspaceDir determines the workspace directory for file tool operations.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDir() string {
	snap, err := config.LoadConfigFromDefaultPath()
	if err == nil && snap != nil {
		dir := config.ResolveAgentWorkspaceDir(&snap.Config)
		if dir != "" {
			return dir
		}
	}
	// Config unavailable — fall back to built-in default.
	return config.ResolveAgentWorkspaceDir(nil)
}

// resolveDenebDir returns the path to ~/.deneb.
func resolveDenebDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deneb")
	}
	return "/tmp/deneb"
}

// StartMonitoring starts the watchdog and channel health monitor goroutines.
func (s *Server) StartMonitoring(ctx context.Context) {
	// Gateway self-watchdog.
	s.watchdog = monitoring.NewWatchdog(monitoring.WatchdogDeps{
		IsServerListening: func() bool { return s.ready.Load() },
		GetExpectedChannelCount: func() int {
			return len(s.channels.List())
		},
		GetConnectedChannelCount: func() int {
			count := 0
			statusAll := s.channels.StatusAll()
			for _, st := range statusAll {
				if st.Connected {
					count++
				}
			}
			return count
		},
		GetLastActivityAt: func() int64 {
			if s.activity != nil {
				return s.activity.LastActivityAt()
			}
			return 0
		},
		IsAutonomousRunning: func() bool {
			return s.autonomousSvc != nil && s.autonomousSvc.IsRunning()
		},
		OnRestartNeeded: func(reason string) {
			s.logger.Warn("watchdog restart requested, sending SIGUSR1", "reason", reason)
			// Send SIGUSR1 to self to trigger graceful restart via main's signal handler.
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				_ = p.Signal(syscall.SIGUSR1)
			}
		},
	}, monitoring.DefaultWatchdogConfig(), s.logger)
	s.safeGo("watchdog", func() { s.watchdog.Run(ctx) })

	// Channel health monitor.
	s.channelHealth = monitoring.NewChannelHealthMonitor(monitoring.ChannelHealthDeps{
		ListChannelIDs: func() []string {
			return s.channels.List()
		},
		GetChannelStatus: func(id string) string {
			ch := s.channels.Get(id)
			if ch == nil {
				return "unknown"
			}
			st := ch.Status()
			if st.Connected {
				return "running"
			}
			if st.Error != "" {
				return "error"
			}
			return "stopped"
		},
		GetChannelLastEventAt: func(id string) int64 {
			if s.channelEvents != nil {
				return s.channelEvents.LastEventAt(id)
			}
			return 0
		},
		GetChannelStartedAt: func(id string) int64 {
			if s.channelLifecycle != nil {
				return s.channelLifecycle.GetStartedAt(id)
			}
			return 0
		},
		RestartChannel: func(id string) error {
			if s.channelLifecycle == nil {
				return fmt.Errorf("channel lifecycle manager not available")
			}
			s.logger.Info("restarting channel via watchdog", "channel", id)
			restartCtx, restartCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer restartCancel()
			err := s.channelLifecycle.RestartChannel(restartCtx, id)
			if err != nil {
				s.logger.Error("channel restart failed", "channel", id, "error", err)
			} else {
				s.emitChannelEvent(id, hooks.EventChannelConnect, "restarted")
			}
			return err
		},
	}, monitoring.DefaultChannelHealthConfig(), s.logger)
	s.safeGo("channel-health-monitor", func() { s.channelHealth.Run(ctx) })
}

// emitChannelEvent fires the appropriate hook and broadcasts a channels.changed event.
func (s *Server) emitChannelEvent(channelID string, hookEvent hooks.Event, action string) {
	if s.hooks != nil {
		s.safeGo("hooks:"+string(hookEvent), func() {
			s.hooks.Fire(context.Background(), hookEvent, map[string]string{
				"DENEB_CHANNEL_ID": channelID,
			})
		})
	}
	s.broadcaster.Broadcast("channels.changed", map[string]any{
		"channelId": channelID,
		"action":    action,
		"ts":        time.Now().UnixMilli(),
	})
}

// startProcessPruner runs a background loop that periodically prunes completed
// processes older than 1 hour to prevent unbounded memory growth.
func (s *Server) startProcessPruner(ctx context.Context) {
	if s.processes == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruned := s.processes.Prune(1 * time.Hour)
				if pruned > 0 {
					s.logger.Info("pruned completed processes", "count", pruned)
				}
			}
		}
	}()
}

// registerBuiltinMethods registers the core RPC methods handled natively in Go.
func (s *Server) registerBuiltinMethods() {
	s.dispatcher.Register("health", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"status": "ok",
			"uptime": time.Since(s.startedAt).Milliseconds(),
		})
		return resp
	})

	s.dispatcher.Register("status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version":     s.version,
			"channels":    s.channels.StatusAll(),
			"sessions":    s.sessions.Count(),
			"connections": s.clientCnt.Load(),
		})
		return resp
	})

	// gateway.identity.get: returns the gateway's identity and runtime information.
	s.dispatcher.Register("gateway.identity.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version": s.version,
			"runtime": "go",
			"uptime":  time.Since(s.startedAt).Milliseconds(),
			"rustFFI": s.rustFFI,
		})
		return resp
	})

	// last-heartbeat: returns the last heartbeat timestamp.
	s.dispatcher.Register("last-heartbeat", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var ts int64
		if s.activity != nil {
			ts = s.activity.LastActivityAt()
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"lastHeartbeatMs": ts,
		})
		return resp
	})

	// set-heartbeats: configure heartbeat settings (accepted but no-op in Go gateway;
	// the tick broadcaster runs at a fixed 1000ms interval).
	s.dispatcher.Register("set-heartbeats", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	})

	// system-presence: broadcast a presence event to all connected clients.
	s.dispatcher.Register("system-presence", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var payload any
		if len(req.Params) > 0 {
			var p struct {
				Payload any `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrInvalidRequest, "invalid params"))
			}
			payload = p.Payload
		}
		sent, _ := s.broadcaster.Broadcast("presence", payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// system-event: broadcast an arbitrary system event.
	s.dispatcher.Register("system-event", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// models.list: return provider model list if available.
	s.dispatcher.Register("models.list", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.providers == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{"models": []any{}})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"models": s.providers.List(),
		})
		return resp
	})

	// config.get: returns the resolved runtime config for diagnostics.
	s.dispatcher.Register("config.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.runtimeCfg == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]string{"status": "not_loaded"})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"bindHost":      s.runtimeCfg.BindHost,
			"port":          s.runtimeCfg.Port,
			"authMode":      s.runtimeCfg.AuthMode,
			"tailscaleMode": s.runtimeCfg.TailscaleMode,
		})
		return resp
	})
}

// pluginRegistryAdapter bridges plugin.FullRegistry to the rpc.PluginRegistry interface.
type pluginRegistryAdapter struct {
	registry *plugin.FullRegistry
}

func (a *pluginRegistryAdapter) ListPlugins() []protocol.PluginMeta {
	raw := a.registry.ListPlugins()
	result := make([]protocol.PluginMeta, len(raw))
	for i, p := range raw {
		result[i] = protocol.PluginMeta{
			ID:      p.ID,
			Name:    p.Label,
			Kind:    protocol.PluginKind(p.Kind),
			Version: p.Version,
			Enabled: p.Enabled,
		}
	}
	return result
}

func (a *pluginRegistryAdapter) GetPluginHealth(id string) *protocol.PluginHealthStatus {
	p := a.registry.GetPlugin(id)
	if p == nil {
		return nil
	}
	return &protocol.PluginHealthStatus{
		PluginID: p.ID,
		Healthy:  p.Enabled,
	}
}

// truncateForDedup returns at most maxLen bytes of s for use as a dedup key.
func truncateForDedup(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
