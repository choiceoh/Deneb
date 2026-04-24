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
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
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

	// Start the Telegram plugin synchronously so that RPC serving only
	// becomes available after the channel is ready.
	if s.telegramPlug != nil {
		if err := s.telegramPlug.Start(ctx); err != nil {
			s.logger.Warn("telegram start failed", "error", err)
		} else {
			s.logger.Info("telegram channel started")
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

	// Cron session GC is handled by session.Manager's Kind-based retention
	// (KindCron → 24h) via evictStale(); no separate reaper needed.

	// Create the run state machine to track active agent runs.
	s.runStateMachine = telegram.NewRunStateMachine(ctx, func(patch telegram.StatusPatch) {
		// Skip logging for periodic heartbeat ticks — only log real transitions.
		if s.snapshotStore != nil && patch.ActiveRuns != nil && !patch.Heartbeat {
			s.logger.Debug("run state changed", "activeRuns", *patch.ActiveRuns)
		}
	}, 30*time.Second)

	// Wire the run state machine to the chat handler.
	if s.chatHandler != nil {
		s.chatHandler.SetRunStateMachine(s.runStateMachine)
	}

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

	// 3. Stop cron service.
	if s.cronService != nil {
		s.cronService.Stop()
	}

	// 4. Stop autonomous service (dreaming).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Stop()
	}

	// 5. Cleanup genesis subsystem.
	if s.genesisSvc != nil {
		s.genesisSvc.Stop()
	}
	if s.genesisTracker != nil {
		s.genesisTracker.Close()
	}

	// 6. Stop local AI hub (drains queued requests, cancels in-flight).
	if s.localAIHub != nil {
		s.localAIHub.Shutdown()
	}
	if s.embeddingClient != nil {
		s.embeddingClient.Shutdown()
	}

	// 7. Close task store.
	if s.taskStore != nil {
		s.taskStore.Close()
	}

	// Gmail polling is stopped by autonomous service (registered as periodic task).

	// 9. Stop Telegram plugin.
	if s.telegramPlug != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.telegramPlug.Stop(stopCtx)
		stopCancel()
	}

	// 11. Close run state machine.
	if s.runStateMachine != nil {
		s.runStateMachine.Close()
	}

	// 12. Close chat handler.
	if s.chatHandler != nil {
		s.chatHandler.Close()
	}

	// 13. Close wiki store (FTS database).
	if s.wikiStore != nil {
		s.wikiStore.Close()
	}

	// 14. Stop process manager background goroutine.
	if s.processes != nil {
		s.processes.Stop()
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
