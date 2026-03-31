package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/logging"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

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

	// Propagate server lifecycle context to the chat handler so background
	// goroutines (auto-memory extraction) stop cleanly on shutdown.
	if s.chatHandler != nil {
		s.chatHandler.SetShutdownCtx(ctx)
	}

	// Auto-start all registered channel plugins synchronously so that RPC
	// serving only becomes available after all channels are ready.
	// Running in a goroutine (as before) caused requests to be routed to
	// plugins that had not yet completed Start(), leading to spurious errors.
	if s.channelLifecycle != nil {
		if errs := s.channelLifecycle.StartAll(ctx); len(errs) > 0 {
			for id, err := range errs {
				s.logger.Warn("channel auto-start failed", "channel", id, "error", err)
			}
		}
	}

	// Start persistent cron service (loads jobs from disk, schedules with delivery).
	if s.cronService != nil {
		s.safeGo("cron-service-start", func() {
			if err := s.cronService.Start(ctx); err != nil {
				s.logger.Error("cron service start failed", "error", err)
			}
		})
	}

	// Mark ready only after all channel plugins have had a chance to start.
	s.ready.Store(true)

	// Restore persisted Telegram sessions to the in-memory session manager.
	s.safeGo("session-restore", func() {
		s.restoreAndWakeSessions(ctx)
	})

	// Start autonomous service (dreaming lifecycle).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Start()
	}

	// Gmail polling is managed by the autonomous service (registered in initGmailPoll).

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

	if s.OnListening != nil {
		s.OnListening(ln.Addr())
	}

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

	// 6. Stop cron scheduler and service.
	if s.cronService != nil {
		s.cronService.Stop()
	}
	if s.cron != nil {
		s.cron.Close()
	}

	// 6b. Stop autonomous service (dreaming).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Stop()
	}

	// 6c. Stop autoresearch runner.
	if s.autoresearchRunner != nil {
		s.autoresearchRunner.Stop()
	}

	// Gmail polling is stopped by autonomous service (registered as periodic task).

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

	// 11b. Stop process manager background goroutine.
	if s.processes != nil {
		s.processes.Stop()
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
	if s.snapshotLifecycleUnsub != nil {
		s.snapshotLifecycleUnsub()
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
