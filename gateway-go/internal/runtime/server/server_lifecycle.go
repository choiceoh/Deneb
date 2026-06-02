package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
)

// initAndListen creates the HTTP server, binds to the address, and starts
// background subsystems (tick broadcaster, monitoring, process pruner, hooks).
// Shared by Run and StartAndListen to avoid duplicating the startup sequence.
func (s *Server) initAndListen(ctx context.Context) (net.Listener, error) {
	// Lifecycle context was initialised in New() so that background
	// goroutines launched before initAndListen runs (e.g. checkpoint GC
	// in New) can read it race-free via ShutdownCtx(). Here we only need
	// to propagate the caller's parent-ctx cancellation into the already-
	// live lifecycle context. Running the forwarder on a detached goroutine
	// keeps initAndListen lock-free and preserves the doShutdown() path.
	//
	// Capture parentCtx by value before the subsequent `ctx = s.lifecycleCtx`
	// reassignment so the closure below reads the original caller context,
	// not the reassigned local. Otherwise -race flags the closure's read
	// against the local-var reassignment as a data race.
	parentCtx := ctx
	if parentCtx != nil && parentCtx.Done() != nil {
		lifecycleDone := s.lifecycleCtx.Done()
		cancelFn := s.lifecycleCancel
		s.safeGo("lifecycle-parent-cancel-forwarder", func() {
			select {
			case <-parentCtx.Done():
				if cancelFn != nil {
					cancelFn()
				}
			case <-lifecycleDone:
			}
		})
	}
	ctx = s.lifecycleCtx

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
	s.StartMonitoring(ctx)
	s.startProcessPruner(ctx)
	s.sessions.StartGC(ctx)

	// Propagate server lifecycle context to the chat handler so background
	// goroutines (auto-memory extraction) stop cleanly on shutdown.
	if s.chatHandler != nil {
		s.chatHandler.SetShutdownCtx(ctx)
	}

	// Start persistent cron service (loads jobs from disk, schedules with delivery).
	if s.cronService != nil {
		s.safeGo("cron-service-start", func() {
			if err := s.cronService.Start(ctx); err != nil {
				s.logger.Error("cron service start failed", "error", err)
			}
		})
	}

	// Cron session GC is handled by session.Manager's Kind-based retention
	// (KindCron → 24h) via evictStale(); no separate reaper needed.

	// Mark ready only after all channel plugins have had a chance to start.
	s.ready.Store(true)

	// Restore persisted Telegram sessions to the in-memory session manager,
	// then re-enqueue any runs that were interrupted by a crash or restart.
	// Both phases run in one goroutine so the ordering is fixed — auto-resume
	// reads the sessions that restoreAndWakeSessions just populated.
	s.safeGo("session-restore", func() {
		s.restoreAndWakeSessions(ctx)
		s.autoResumeInterruptedRuns(ctx)
	})

	// Start autonomous service (dreaming lifecycle).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Start()
	}

	// Start background task maintenance loop (orphan recovery, cleanup).
	if s.taskRegistry != nil {
		sessionChecker := func(key string) bool {
			return s.sessions.Get(key) != nil
		}
		tasks.StartMaintenanceLoop(ctx, s.taskRegistry, sessionChecker, s.logger)
	}

	// Gmail polling is managed by the autonomous service (registered in initGmailPoll).

	return ln, nil
}

// Run starts the server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := s.initAndListen(ctx)
	if err != nil {
		return err
	}

	addrStr := ln.Addr().String()
	s.boundAddr.Store(&addrStr)

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

	// 1. Stop accepting new connections.
	var httpErr error
	if s.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpErr = s.httpServer.Shutdown(shutdownCtx)
	}

	// 2. Stop gateway event subscriptions (bounded to avoid hanging).
	if s.gatewaySubs != nil {
		stopWithTimeout(5*time.Second, "gatewaySubs.Stop", s.logger, s.gatewaySubs.Stop)
	}

	// 3. Stop cron service. The bounded context cancels every in-flight
	// executor (scheduler, recovery, async POST /api/cron/run) and waits
	// for them so that downstream subsystems (Telegram plugin, chat
	// handler) are not torn down while a cron run is still using them.
	// See issue #1633.
	if s.cronService != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.cronService.StopCtx(stopCtx)
		stopCancel()
	}

	// Every drain below is wrapped in stopWithTimeout. doShutdown closes the
	// HTTP listener first (step 1), so any step that blocks indefinitely keeps
	// the gateway un-serving until it returns; an unbounded drain therefore
	// stretches the listener-closed window up to the lifecycle watchdog's grace
	// (45s) before the process is force-exited. Bounding each step keeps that
	// window short — the watchdog stays a last resort, not the routine path.

	// 4. Stop autonomous service (dreaming).
	if s.autonomousSvc != nil {
		stopWithTimeout(10*time.Second, "autonomousSvc.Stop", s.logger, s.autonomousSvc.Stop)
	}

	// 5. Cleanup genesis subsystem.
	if s.genesisSvc != nil {
		stopWithTimeout(5*time.Second, "genesisSvc.Stop", s.logger, s.genesisSvc.Stop)
	}
	if s.genesisTracker != nil {
		stopWithTimeout(5*time.Second, "genesisTracker.Close", s.logger, func() { _ = s.genesisTracker.Close() })
	}

	// 6. Stop local AI hub (drains queued requests, cancels in-flight). Bounded
	// because the drain can block on an in-flight inference to a stalled local
	// model server (vLLM under memory pressure) — the most likely cause of the
	// shutdown hang that wedged the gateway before the watchdog + these caps.
	if s.localAIHub != nil {
		stopWithTimeout(10*time.Second, "localAIHub.Shutdown", s.logger, s.localAIHub.Shutdown)
	}
	if s.embeddingClient != nil {
		stopWithTimeout(10*time.Second, "embeddingClient.Shutdown", s.logger, s.embeddingClient.Shutdown)
	}

	// 7. Close task store.
	if s.taskStore != nil {
		stopWithTimeout(5*time.Second, "taskStore.Close", s.logger, func() { _ = s.taskStore.Close() })
	}

	// Gmail polling is stopped by autonomous service (registered as periodic task).

	// 11. Close chat handler.
	if s.chatHandler != nil {
		stopWithTimeout(5*time.Second, "chatHandler.Close", s.logger, s.chatHandler.Close)
	}

	// 13. Close wiki store (FTS database).
	if s.wikiStore != nil {
		stopWithTimeout(5*time.Second, "wikiStore.Close", s.logger, func() { _ = s.wikiStore.Close() })
	}

	// 14. Stop process manager background goroutine.
	if s.processes != nil {
		stopWithTimeout(5*time.Second, "processes.Stop", s.logger, s.processes.Stop)
	}

	// 15. ACP cleanup: persist bindings, registry, and unsubscribe lifecycle sync.
	if s.acpDeps != nil && s.acpDeps.BindingStore != nil && s.acpDeps.Bindings != nil {
		if err := s.acpDeps.BindingStore.SyncFromService(s.acpDeps.Bindings); err != nil {
			s.logger.Warn("failed to persist ACP bindings on shutdown", "error", err)
		}
	}
	if s.acpDeps != nil && s.acpDeps.RegistryStore != nil && s.acpDeps.Registry != nil {
		if err := s.acpDeps.RegistryStore.SyncFromRegistry(s.acpDeps.Registry); err != nil {
			s.logger.Warn("failed to persist ACP registry on shutdown", "error", err)
		}
	}
	if s.acpLifecycleUnsub != nil {
		s.acpLifecycleUnsub()
	}
	if s.acpResultInjectionUnsub != nil {
		s.acpResultInjectionUnsub()
	}
	if s.snapshotLifecycleUnsub != nil {
		s.snapshotLifecycleUnsub()
	}
	if s.checkpointLifecycleUnsub != nil {
		s.checkpointLifecycleUnsub()
	}
	if s.spilloverLifecycleUnsub != nil {
		s.spilloverLifecycleUnsub()
	}
	if s.runMarkerUnsub != nil {
		s.runMarkerUnsub()
	}

	// 13. Cancel lifecycle context so remaining background goroutines exit,
	// then wait for them to finish.
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	stopWithTimeout(5*time.Second, "bgWg.Wait", s.logger, s.bgWg.Wait)

	return httpErr
}

// stopWithTimeout runs fn in a goroutine and waits up to d for it to finish.
// Logs a warning with the given label if the timeout is exceeded.
func stopWithTimeout(d time.Duration, label string, logger *slog.Logger, fn func()) {
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		logger.Warn(label + " timed out")
	}
}
