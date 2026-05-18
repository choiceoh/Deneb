package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Start loads jobs from the store, runs missed-job recovery, and spawns
// the long-lived scheduler worker goroutine. Idempotent — calling Start
// twice is a no-op after the first.
//
// One-shot "at", recurring "every", and "cron" expression jobs all use
// the same NextRunAtMs-driven worker.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()

	if s.running {
		s.mu.Unlock()
		return nil
	}

	storeData, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("load cron store: %w", err)
	}

	// Ensure NextRunAtMs is set for all enabled jobs.
	scheduled := 0
	nowMs := time.Now().UnixMilli()
	for i := range storeData.Jobs {
		job := &storeData.Jobs[i]
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs <= 0 {
			job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, nowMs)
			if job.State.NextRunAtMs > 0 {
				if err := s.store.UpdateJobState(job.ID, job.State); err != nil {
					s.logger.Warn("cron boot state init persist failed",
						"id", job.ID, "error", err)
				}
			}
		}
		scheduled++
	}

	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		nrm := job.State.NextRunAtMs
		overdue := nrm > 0 && nrm <= nowMs
		s.logger.Info("cron boot job status",
			"id", job.ID,
			"nextRunAtMs", nrm,
			"overdue", overdue,
			"overdueMs", nowMs-nrm,
		)
	}

	// Collect jobs that should have fired during downtime (pre-advance their
	// NextRunAtMs while still under s.mu). Executors are spawned AFTER
	// the lock is released, so they never race with code holding s.mu.
	toRecover := s.collectMissedJobsLocked(storeData)

	// Reset stopCh so a second Start after Stop works. loopDone is fresh
	// per-loop so Stop can wait on it. runCtx is derived from the caller
	// ctx so cancellation still flows through the parent, but Stop also
	// has its own cancel handle so it can abort in-flight executors even
	// when the caller's ctx is still alive.
	s.stopCh = make(chan struct{})
	s.loopDone = make(chan struct{})
	s.runCtx, s.runCancel = context.WithCancel(ctx)
	s.running = true
	runCtx := s.runCtx
	s.mu.Unlock()

	// Now safe to spawn recovery executors against the service-owned ctx.
	s.spawnRecoverExecutors(runCtx, toRecover)

	// Spawn the panic-recovered worker goroutine. It runs until runCtx is
	// cancelled (either by the parent ctx finishing or by Stop closing it)
	// or Stop closes stopCh. signalWake() nudges it when state changes
	// (Add/Update/Remove/Run/applyJobResult).
	safego.GoWithSlog(s.logger, "cron-scheduler-loop", func() {
		s.runSchedulerLoop(runCtx)
	})

	if scheduled > 0 {
		s.logger.Info("cron service started", "scheduled", scheduled)
	}
	return nil
}

// Stop signals the scheduler loop to exit, cancels in-flight executor
// contexts, and waits for them with a built-in 10s deadline. Equivalent
// to calling StopCtx with a 10s timeout — kept for callers (mostly tests)
// that don't need a custom deadline. Idempotent.
func (s *Service) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.StopCtx(ctx)
}

// StopCtx signals the scheduler loop to exit, cancels every in-flight
// executor goroutine (scheduled, recovery, and async EnqueueRun), and
// waits for them up to ctx.Done(). The caller is expected to pass a
// bounded context so a stuck agent turn cannot block shutdown forever.
//
// Synchronous Run callers (e.g. the cron.run RPC handler) are not
// tracked here — they own their own request context and are bounded by
// the caller's lifetime.
//
// Idempotent: calling on a stopped service returns immediately.
func (s *Service) StopCtx(ctx context.Context) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	loopDone := s.loopDone
	runCancel := s.runCancel
	s.runCancel = nil
	s.mu.Unlock()

	// Signal in-flight executors to exit. They observe ctx.Done() on the
	// service-owned runCtx; goroutines that propagate it (agent turn, HTTP
	// calls, etc.) get woken up immediately.
	if runCancel != nil {
		runCancel()
	}

	// Wait for the scheduler loop to exit so a subsequent Start sees a
	// clean slate. The loop's own 5s budget guards against caller-supplied
	// deadlines that are longer than the loop should ever take.
	if loopDone != nil {
		select {
		case <-loopDone:
		case <-ctx.Done():
			s.logger.Warn("cron scheduler loop did not exit before deadline", "error", ctx.Err())
		case <-time.After(5 * time.Second):
			s.logger.Warn("cron scheduler loop did not exit within 5s")
		}
	}

	// Wait for in-flight executors to finish, bounded by the caller's
	// deadline. Without this, doShutdown could tear down dependencies
	// (Telegram plugin, chat handler, etc.) while a cron run is still
	// using them — see issue #1633.
	waitDone := make(chan struct{})
	go func() {
		s.inFlight.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-ctx.Done():
		s.logger.Warn("cron in-flight executors did not finish before deadline", "error", ctx.Err())
	}

	s.logger.Info("cron service stopped")
}

// Status returns the service status from the store.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	running := s.running
	nextWake := s.nextWakeAtMs
	s.mu.Unlock()

	storeData, _ := s.store.Load()
	enabledCount := 0
	if storeData != nil {
		for _, j := range storeData.Jobs {
			if j.Enabled {
				enabledCount++
			}
		}
	}

	return ServiceStatus{
		Running:     running,
		TaskCount:   enabledCount,
		NextRunAtMs: nextWake,
	}
}
