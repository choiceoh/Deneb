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
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
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
	addr        string
	httpServer  *http.Server
	dispatcher  *rpc.Dispatcher
	sessions    *session.Manager
	channels         *channel.Registry
	channelLifecycle *channel.LifecycleManager
	keyCache         *session.KeyCache
	dedupe           *dedupe.Tracker
	broadcaster *events.Broadcaster
	processes   *process.Manager
	cron        *cron.Scheduler
	daemon      *daemon.Daemon
	hooks       *hooks.Registry
	runtimeCfg    *config.GatewayRuntimeConfig
	authValidator *auth.Validator
	clients     sync.Map // connID -> *WsClient
	clientCnt   atomic.Int32
	startedAt   time.Time
	version     string
	rustFFI     bool // true when Rust FFI is available
	logger      *slog.Logger
	ready       atomic.Bool
	shutdownOnce sync.Once

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

	// Phase 3: Advanced workflow subsystems.
	approvals  *approval.Store
	nodes      *node.Manager
	devices    *device.Manager
	agents     *agent.Store
	skills     *skill.Manager
	wizardEng  *wizard.Engine
	secrets    *secret.Resolver
	talkState  *talk.State

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

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.dispatcher.UseMiddleware(middleware.Logging(s.logger))
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
			Deps: rpc.Deps{
				Sessions: s.sessions,
				Channels: s.channels,
			},
			Providers: s.providers,
		})
	}

	// Initialize plugin full registry and register RPC methods.
	s.pluginFullRegistry = plugin.NewFullRegistry(s.logger)
	rpc.RegisterPluginMethods(s.dispatcher, rpc.PluginDeps{
		Deps:           rpc.Deps{Sessions: s.sessions, Channels: s.channels},
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
	s.ready.Store(true)
	s.startTickBroadcaster(ctx)
	s.StartMonitoring(ctx)
	s.startProcessPruner(ctx)

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
	s.logger.Info("gateway server shutting down")

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
		client.conn.Close(websocket.StatusGoingAway, "server shutting down")
		return true
	})

	// 4. Stop gateway event subscriptions.
	if s.gatewaySubs != nil {
		s.gatewaySubs.Stop()
	}

	// 5. Stop dedupe background GC.
	s.dedupe.Close()

	// 6. Stop cron scheduler.
	if s.cron != nil {
		s.cron.Close()
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

	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"version":         s.version,
		"runtime":         "go",
		"uptime":          time.Since(s.startedAt).Milliseconds(),
		"connections":     s.clientCnt.Load(),
		"sessions":        s.sessions.Count(),
		"rust_core":       s.rustFFI,
		"auth_mode":       authMode,
		"providers":       providerCount,
		"processes":       activeProcesses,
		"cronTasks":       cronTasks,
		"hooks":           hooksCount,
		"channelHealth":   channelHealthSummary,
	})
}

// handleReady responds with readiness status.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	ready := s.ready.Load()
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	s.writeJSON(w, status, map[string]any{"ready": ready})
}

// writeJSON encodes v as JSON to the response writer, logging any encoding errors.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("json encode error", "error", err)
	}
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
	s.writeJSON(w, http.StatusOK, map[string]string{
		"service": "deneb-gateway",
		"runtime": "go",
		"version": s.version,
	})
}

// registerExtendedMethods registers Phase 2 RPC methods (process, cron, hooks, agent).
func (s *Server) registerExtendedMethods() {
	rpc.RegisterExtendedMethods(s.dispatcher, rpc.ExtendedDeps{
		Deps: rpc.Deps{
			Sessions:    s.sessions,
			Channels:    s.channels,
			GatewaySubs: s.gatewaySubs,
		},
		Processes:   s.processes,
		Cron:        s.cron,
		Hooks:       s.hooks,
		Broadcaster: s.broadcaster,
	})

	// Provider methods.
	rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
		Deps: rpc.Deps{
			Sessions: s.sessions,
			Channels: s.channels,
		},
		Providers:   s.providers,
		AuthManager: s.authManager,
	})

	// Tool methods.
	rpc.RegisterToolMethods(s.dispatcher, rpc.ToolDeps{
		Deps: rpc.Deps{
			Sessions: s.sessions,
			Channels: s.channels,
		},
		Processes: s.processes,
	})

	// Session state methods (patch/reset/preview/resolve/compact).
	var sessionCompressor *transcript.Compressor
	if s.transcript != nil {
		sessionCompressor = transcript.NewCompressor(transcript.DefaultCompactionConfig(), s.logger)
	}
	sessionDeps := rpc.SessionDeps{
		Deps: rpc.Deps{
			Sessions:    s.sessions,
			Channels:    s.channels,
			GatewaySubs: s.gatewaySubs,
		},
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
		transcriptStore = chat.NewFileTranscriptStore(transcriptDir)
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker

	// Initialize Aurora compaction store.
	auroraStore, err := aurora.NewStore(aurora.DefaultStoreConfig(), s.logger)
	if err != nil {
		s.logger.Warn("aurora store unavailable, compaction will use legacy fallback", "error", err)
	} else {
		chatCfg.AuroraStore = auroraStore
		s.logger.Info("aurora compaction store initialized")
	}

	// Resolve default model from config; fall back to hardcoded default.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)

	// Resolve workspace directory for file tool operations.
	workspaceDir := resolveWorkspaceDir()
	s.logger.Info("resolved agent workspace directory", "workspaceDir", workspaceDir)

	// Register core tools (file I/O, exec, process, stubs for others).
	chat.RegisterCoreTools(chatCfg.Tools, s.processes, workspaceDir, s.cron)
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

	// Stub handlers for methods not available in standalone mode.
	stubUnavailable := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, req.Method+" not available in standalone mode"))
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
		opts := telegram.SendOptions{ParseMode: "HTML"}
		html := telegram.FormatHTML(text)
		_, err = telegram.SendText(ctx, client, chatID, html, opts)
		return err
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
		return "zai/glm-5-turbo"
	}
	var root struct {
		Agents struct {
			DefaultModel string          `json:"defaultModel"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for model", "error", err)
		return "zai/glm-5-turbo"
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
	return "zai/glm-5-turbo"
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
			"bindHost":       s.runtimeCfg.BindHost,
			"port":           s.runtimeCfg.Port,
			"authMode":       s.runtimeCfg.AuthMode,
			"tailscaleMode":  s.runtimeCfg.TailscaleMode,
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
