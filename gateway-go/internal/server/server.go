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
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/bridge"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/dedupe"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
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
	dedupe      *dedupe.Tracker
	broadcaster *events.Broadcaster
	processes   *process.Manager
	cron        *cron.Scheduler
	daemon      *daemon.Daemon
	hooks       *hooks.Registry
	clients     sync.Map // connID -> *WsClient
	clientCnt   atomic.Int32
	startedAt   time.Time
	version     string
	rustFFI     bool // true when Rust FFI is available
	logger      *slog.Logger
	ready       atomic.Bool
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
	s.processes = process.NewManager(s.logger)
	s.cron = cron.NewScheduler(s.logger)
	s.hooks = hooks.NewRegistry(s.logger)
	s.channelLifecycle = channel.NewLifecycleManager(s.channels, s.logger)

	s.dispatcher = rpc.NewDispatcher(s.logger)
	s.registerBuiltinMethods()
	rpc.RegisterBuiltinMethods(s.dispatcher, rpc.Deps{
		Sessions:         s.sessions,
		Channels:         s.channels,
		ChannelLifecycle: s.channelLifecycle,
	})
	s.registerExtendedMethods()
	return s
}

// SetBridge sets the Plugin Host bridge for forwarding unhandled RPC methods.
func (s *Server) SetBridge(b *bridge.PluginHost) {
	s.bridge = b
	s.dispatcher.SetForwarder(b)
}

// Run starts the server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	mux := s.buildMux()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
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
	mux := s.buildMux()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
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

	// 4. Stop dedupe background GC.
	s.dedupe.Close()

	// 5. Stop cron scheduler.
	if s.cron != nil {
		s.cron.Close()
	}

	// 6. Fire gateway.stop hooks.
	if s.hooks != nil {
		s.hooks.Fire(context.Background(), hooks.EventGatewayStop, nil)
	}

	// 7. Stop all channel plugins.
	if s.channelLifecycle != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.channelLifecycle.StopAll(stopCtx)
		stopCancel()
	}

	// 8. Close Plugin Host bridge last (in-flight forwards finish first).
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

	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"version":     s.version,
		"runtime":     "go",
		"uptime":      time.Since(s.startedAt).Milliseconds(),
		"connections": s.clientCnt.Load(),
		"sessions":    s.sessions.Count(),
		"bridge":      bridgeStatus,
		"rust_core":   s.rustFFI,
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
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
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
			Sessions: s.sessions,
			Channels: s.channels,
		},
		Processes: s.processes,
		Cron:      s.cron,
		Hooks:     s.hooks,
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

// Broadcaster returns the event broadcaster for external use.
func (s *Server) Broadcaster() *events.Broadcaster {
	return s.broadcaster
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
}
