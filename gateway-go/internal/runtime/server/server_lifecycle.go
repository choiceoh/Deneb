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

	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/sdsocket"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// DefaultTurnDeadline is the end-to-end budget for one user turn.
const DefaultTurnDeadline = 5 * time.Minute

// chatDrainTimeout lets a detached native stream use its 6-minute backstop
// before shutdown cancels the server lifecycle. New runs are rejected while
// this wait is active, so deploys do not extend indefinitely under traffic.
const chatDrainTimeout = chatport.InteractiveTurnDeadline

// initAndListen creates the HTTP server, binds to the address, and starts
// background subsystems (tick broadcaster, monitoring, session GC, hooks).
// Shared by Run and StartAndListen to avoid duplicating the startup sequence.
func (s *Server) initAndListen(ctx context.Context) (net.Listener, error) {
	// Run observes the caller context and enters doShutdown itself. Do not wire
	// that context directly into lifecycleCtx: native streaming turns are
	// intentionally detached from their socket and use lifecycleCtx so a deploy
	// can stop admission, drain them, and only then cancel background work.
	// StartAndListen has the same explicit contract: its caller must call Close.
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	ctx = s.lifecycleCtx

	mux := s.buildMux()

	// Prefer the systemd socket-activated listener (deneb-http.socket,
	// FileDescriptorName=http): systemd keeps the socket bound across SIGUSR1
	// hot-swaps, so connections arriving during a deploy queue in the kernel
	// backlog instead of getting refused. Opt-in — without the socket unit this
	// resolves false and the gateway binds s.addr itself, exactly as before.
	var ln net.Listener
	if sdln, ok := sdsocket.Listener("http"); ok {
		s.logger.Info("http listener: systemd socket-activated (survives hot-swaps)", "addr", sdln.Addr().String())
		ln = sdln
	} else {
		var lc net.ListenConfig
		bound, err := lc.Listen(ctx, "tcp", s.addr)
		if err != nil {
			return nil, fmt.Errorf("failed to listen on %s: %w", s.addr, err)
		}
		ln = bound
	}

	s.httpServer = &http.Server{
		// withCORS lets the browser-based workstation (Andromeda) reach the
		// miniapp.* surface; it's a no-op for Origin-less native clients.
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout is a DoS backstop: a peer that opens a connection and then
		// stops reading the response can otherwise pin a goroutine + connection
		// indefinitely. It must exceed DefaultTurnDeadline because the blocking
		// miniapp.chat.send RPC writes its response only after the full agent
		// turn completes. Long-lived streaming routes (SSE chat/stream and the
		// persistent events channel) and large file downloads (APK, Gmail
		// attachments) legitimately outlast this and lift it per-response via
		// the disableWriteDeadline helpers in nativeapi/appupdate; the backstop
		// still covers every ordinary handler.
		WriteTimeout: 2 * DefaultTurnDeadline,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	s.startedAt = time.Now()
	s.StartMonitoring(ctx)
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

	// SparkFleet readiness: probe the control plane for down GPU backends and
	// surface them (startup warning + /healthz), refreshing on a ticker.
	if s.fleet != nil {
		s.safeGo("sparkfleet-readiness", func() {
			s.fleet.Run(ctx)
		})
	}

	// Cron session GC is handled by session.Manager's Kind-based retention
	// (KindCron → 24h) via evictStale(); no separate reaper needed.

	// Mark ready only after all channel plugins have had a chance to start.
	s.ready.Store(true)

	// Restore persisted user sessions to the in-memory session manager,
	// then re-enqueue any runs that were interrupted by a crash or restart.
	// Both phases run in one goroutine so the ordering is fixed — auto-resume
	// reads the sessions that restoreAndWakeSessions just populated.
	// Keep conversation titles durable across the frequent hot-swap restarts:
	// a periodic sweep persists session labels to the sidecar store that
	// restoreAndWakeSessions reads back (session_labels.go). Started here — not
	// inside restore — so labels of sessions created AFTER startup persist even
	// when nothing was restored.
	s.startSessionLabelPersistence()

	s.safeGo("session-restore", func() {
		s.restoreAndWakeSessions(ctx)
		// Restore the persisted per-session prompt snapshots into the live
		// stores, now that the session manager knows which keys are alive. Done
		// here (same goroutine, right after restore) so the prune-at-load drops
		// snapshots whose session no longer exists, and the first post-restart
		// turn of each live session reuses its frozen bytes (no APC re-prefill).
		if n := chat.LoadPromptSnapshots(func(key string) bool { return s.sessions.Get(key) != nil }); n > 0 {
			s.logger.Info("prompt snapshot restore: restored sessions", "count", n)
		}
		s.autoResumeInterruptedRuns(ctx)
	})

	// Start autonomous service (dreaming lifecycle).
	if s.autonomousSvc != nil {
		s.autonomousSvc.Start()
	}

	// Watch our OWN improvement loops for silent death — the dreamer/skill-
	// curator/config-audit rot nobody noticed. Same gated push path as the fleet
	// hook; stops with ctx.
	go s.runObservatoryWatchdog(ctx)

	// Gmail polling is managed by the autonomous service (registered in initGmailPoll).

	return ln, nil
}

// Run starts the server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := s.initAndListen(ctx)
	if err != nil {
		return err
	}
	s.markListening(ln.Addr())

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
	s.markListening(ln.Addr())

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve error", "error", err)
		}
	}()

	return ln.Addr(), nil
}

func (s *Server) markListening(addr net.Addr) {
	if addr == nil {
		return
	}
	addrStr := addr.String()
	s.boundAddr.Store(&addrStr)
	if s.OnListening != nil {
		s.OnListening(addr)
	}
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

	// 1. Stop admitting new chat runs, then let every already-accepted turn
	// finish while the old listener and detached stream contexts are still live.
	// If a run exceeds the turn budget, continue teardown and let lifecycle
	// cancellation below abort it rather than wedging the deploy forever.
	if s.chatHandler != nil {
		drainStarted := time.Now()
		s.logger.Info("chat admission closed; draining accepted runs", "timeout", chatDrainTimeout)
		drainCtx, drainCancel := context.WithTimeout(context.Background(), chatDrainTimeout)
		if err := s.chatHandler.BeginDrain(drainCtx); err != nil {
			s.logger.Warn("chat drain exceeded shutdown budget; cancelling remaining runs",
				"timeout", chatDrainTimeout, "error", err)
		} else {
			s.logger.Info("chat drain complete", "elapsed", time.Since(drainStarted))
		}
		drainCancel()
	}

	// 2. Cancel the server lifecycle only after the chat drain. This closes the
	// persistent native events SSE and stops background producers, allowing the
	// HTTP server to shut down promptly without sacrificing an in-flight reply.
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}

	// 3. Stop accepting new connections and drain any non-chat HTTP handlers.
	var httpErr error
	if s.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		httpErr = s.httpServer.Shutdown(shutdownCtx)
		cancel()
	}

	// 4. Stop gateway event subscriptions (bounded to avoid hanging).
	if s.gatewaySubs != nil {
		stopWithTimeout(5*time.Second, "gatewaySubs.Stop", s.logger, s.gatewaySubs.Stop)
	}

	// 5. Stop cron service. The bounded context cancels every in-flight
	// executor (scheduler, recovery, async POST /api/cron/run) and waits
	// for them so that downstream subsystems (the chat handler) are not
	// torn down while a cron run is still using them.
	// See issue #1633.
	if s.cronService != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.cronService.StopCtx(stopCtx)
		stopCancel()
	}

	// Every drain below is wrapped in stopWithTimeout. doShutdown closes the
	// HTTP listener above, so any step that blocks indefinitely keeps
	// the gateway un-serving until it returns; an unbounded drain therefore
	// stretches the listener-closed window up to the lifecycle watchdog's grace
	// before the process is force-exited. Bounding each step keeps that
	// window short — the watchdog stays a last resort, not the routine path.

	// 6. Stop autonomous service (dreaming).
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
	if s.mailStore != nil {
		stopWithTimeout(5*time.Second, "mailStore.Close", s.logger, func() { _ = s.mailStore.Close() })
	}
	if s.workFeedStore != nil {
		stopWithTimeout(5*time.Second, "workFeedStore.Close", s.logger, func() { _ = s.workFeedStore.Close() })
	}
	if s.embeddingClient != nil {
		stopWithTimeout(10*time.Second, "embeddingClient.Shutdown", s.logger, s.embeddingClient.Shutdown)
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
		// Process cancellation allows a 5s SIGTERM grace before SIGKILL. Keep
		// the outer shutdown bound above that window so Stop can finish its join
		// instead of leaving the cleanup goroutine behind at the same deadline.
		stopWithTimeout(10*time.Second, "processes.Stop", s.logger, s.processes.Stop)
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

	// The lifecycle context was cancelled immediately after the chat drain;
	// join the background goroutines after their owned subsystems are closed.
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
