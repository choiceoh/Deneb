// Package server implements the HTTP + WebSocket gateway server.
//
// This replaces the scaffolding from Phase 0/1 with a working gateway
// server that handles health endpoints, WebSocket connections with the
// full handshake protocol, and RPC dispatch.
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/bridge"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
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
	bridge           *bridge.PluginHost
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
	authRateLimiter *auth.AuthRateLimiter
	watchdog        *monitoring.Watchdog
	channelHealth   *monitoring.ChannelHealthMonitor
	activity        *monitoring.ActivityTracker
	channelEvents   *monitoring.ChannelEventTracker
	vegaClient      *vega.Client
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

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.registerBuiltinMethods()
	rpc.RegisterBuiltinMethods(s.dispatcher, rpc.Deps{
		Sessions:         s.sessions,
		Channels:         s.channels,
		ChannelLifecycle: s.channelLifecycle,
		GatewaySubs:      s.gatewaySubs,
	})
	s.registerExtendedMethods()
	s.registerPhase2Methods()

	// Wire provider RPC methods if a provider registry is configured.
	if s.providers != nil {
		adapter := provider.NewProtocolAdapter(s.providers)
		rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
			Deps: rpc.Deps{
				Sessions: s.sessions,
				Channels: s.channels,
			},
			ProviderCatalog: adapter,
		})
	}

	return s
}

// SetBridge sets the Plugin Host bridge for forwarding unhandled RPC methods.
// Also wires bridge event forwarding to the chat handler and broadcaster.
func (s *Server) SetBridge(b *bridge.PluginHost) {
	s.bridge = b
	s.dispatcher.SetForwarder(b)

	// Wire raw broadcast to chat handler for streaming event relay.
	if s.chatHandler != nil {
		s.chatHandler.SetBroadcastRaw(func(event string, data []byte) int {
			return s.broadcaster.BroadcastRaw(event, data)
		})
	}

	// Wire process approval callback via bridge forward.
	if s.processes != nil {
		s.processes.SetApprover(func(req process.ExecRequest) bool {
			if !b.IsRunning() {
				return false
			}
			// Truncate command in approval payload to prevent OOM on marshal.
			cmd := req.Command
			if len(cmd) > 4096 {
				cmd = cmd[:4096]
			}
			params, err := json.Marshal(map[string]any{
				"id":      req.ID,
				"command": cmd,
				"args":    req.Args,
			})
			if err != nil {
				s.logger.Error("approval params marshal failed", "id", req.ID, "error", err)
				return false
			}
			approvalReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     "approval-" + req.ID,
				Method: "exec.approve",
				Params: params,
			}
			approvalCtx, approvalCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer approvalCancel()
			resp, err := b.Forward(approvalCtx, approvalReq)
			if err != nil {
				s.logger.Warn("process approval forward failed", "id", req.ID, "error", err)
				return false
			}
			return resp.OK
		})
	}

	// Wire bridge events: chat events go to chatHandler, lifecycle/agent events
	// to gatewaySubs, and everything else to broadcaster.
	// Wrapped in panic recovery so a single bad event doesn't kill the bridge read loop.
	b.SetEventHandler(func(ev *protocol.EventFrame) {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic in bridge event handler", "event", ev.Event, "panic", r)
			}
		}()

		// Track activity on all bridge events.
		if s.activity != nil {
			s.activity.Touch()
		}

		switch {
		case s.chatHandler != nil && (ev.Event == "chat" || ev.Event == "chat.delta"):
			s.chatHandler.HandleBridgeEvent(ev)

		case ev.Event == "agent" || ev.Event == "agent.event":
			if s.gatewaySubs != nil {
				s.gatewaySubs.EmitAgent(events.AgentEvent{
					Kind:    ev.Event,
					Payload: ev.Payload,
				})
			}
			s.broadcaster.BroadcastRaw(ev.Event, mustMarshalEvent(ev))

		case ev.Event == "heartbeat":
			if s.gatewaySubs != nil {
				s.gatewaySubs.EmitHeartbeat(events.HeartbeatEvent{
					Ts: time.Now().UnixMilli(),
				})
			}
			s.broadcaster.BroadcastRaw(ev.Event, mustMarshalEvent(ev))

		case ev.Event == "channel.event":
			// Track per-channel event timestamps for health monitoring.
			if s.channelEvents != nil && len(ev.Payload) > 0 {
				var chEvt struct {
					ChannelID string `json:"channelId"`
				}
				if json.Unmarshal(ev.Payload, &chEvt) == nil && chEvt.ChannelID != "" {
					s.channelEvents.Touch(chEvt.ChannelID)
				}
			}
			s.broadcaster.BroadcastRaw(ev.Event, mustMarshalEvent(ev))

		case ev.Event == "session.transcript":
			if s.gatewaySubs != nil && len(ev.Payload) > 0 {
				var update events.TranscriptUpdate
				if json.Unmarshal(ev.Payload, &update) == nil {
					s.gatewaySubs.EmitTranscript(update)
				}
			}

		default:
			s.broadcaster.BroadcastRaw(ev.Event, mustMarshalEvent(ev))
		}
	})
}

// mustMarshalEvent marshals an event frame to JSON bytes.
func mustMarshalEvent(ev *protocol.EventFrame) []byte {
	data, err := json.Marshal(ev)
	if err != nil {
		return []byte("{}")
	}
	return data
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

	// 11. Close Vega client.
	if s.vegaClient != nil {
		s.vegaClient.Close()
	}

	// 12. Close Plugin Host bridge last (in-flight forwards finish first).
	if s.bridge != nil {
		s.bridge.Close()
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

	// Control UI routes.
	// Control UI removed (Phase 0: Rust+Go migration).

	mux.HandleFunc("GET /{$}", s.handleRoot)
	return mux
}

// handleHealth responds with gateway health status including subsystem state.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	bridgeStatus := "not_configured"
	if s.bridge != nil {
		if s.bridge.IsRunning() {
			bridgeStatus = "connected"
		} else {
			bridgeStatus = "disconnected"
		}
	}

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
		"bridge":          bridgeStatus,
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

	dispatchCtx, dispatchCancel := context.WithTimeout(r.Context(), dispatchTimeout)
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

	// Daemon status method.
	s.dispatcher.Register("daemon.status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.daemon == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"state": "not_configured"})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, s.daemon.Status())
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})
}

// SetDaemon sets the daemon manager for lifecycle control.
func (s *Server) SetDaemon(d *daemon.Daemon) {
	s.daemon = d
}

// SetVega sets the Vega MCP client and registers its RPC methods.
func (s *Server) SetVega(client *vega.Client) {
	s.vegaClient = client
	rpc.RegisterVegaMethods(s.dispatcher, rpc.VegaDeps{Client: client})
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
	// Chat methods — forward heavy work to Node.js bridge.
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}
	s.chatHandler = chat.NewHandler(
		s.sessions,
		s.bridge, // may be nil; chat.send will error gracefully
		broadcastFn,
		s.logger,
		chat.DefaultHandlerConfig(),
	)
	rpc.RegisterChatMethods(s.dispatcher, rpc.ChatDeps{Chat: s.chatHandler})

	// Config reload method with bridge forwarding and Go subsystem propagation.
	rpc.RegisterConfigReloadMethod(s.dispatcher, rpc.ConfigReloadDeps{
		Forwarder: s.bridge,
		Logger:    s.logger,
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
			s.logger.Warn("watchdog restart requested", "reason", reason)
			// In production, this would send SIGUSR1 to trigger graceful restart.
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"status": "ok",
			"uptime": time.Since(s.startedAt).Milliseconds(),
		})
		return resp
	})

	s.dispatcher.Register("status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"version":     s.version,
			"channels":    s.channels.StatusAll(),
			"sessions":    s.sessions.Count(),
			"connections": s.clientCnt.Load(),
		})
		return resp
	})

	// config.get: returns the resolved runtime config for diagnostics.
	s.dispatcher.Register("config.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.runtimeCfg == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"status": "not_loaded"})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"bindHost":       s.runtimeCfg.BindHost,
			"port":           s.runtimeCfg.Port,
			"authMode":       s.runtimeCfg.AuthMode,
			"tailscaleMode":  s.runtimeCfg.TailscaleMode,
		})
		return resp
	})
}
