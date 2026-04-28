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
	// per-loop so Stop can wait on it.
	s.stopCh = make(chan struct{})
	s.loopDone = make(chan struct{})
	s.running = true
	s.mu.Unlock()

	// Now safe to spawn recovery executors.
	s.spawnRecoverExecutors(ctx, toRecover)

	// Spawn the panic-recovered worker goroutine. It runs until ctx is
	// cancelled or Stop closes stopCh. signalWake() nudges it when state
	// changes (Add/Update/Remove/Run/applyJobResult).
	safego.GoWithSlog(s.logger, "cron-scheduler-loop", func() {
		s.runSchedulerLoop(ctx)
	})

	if scheduled > 0 {
		s.logger.Info("cron service started", "scheduled", scheduled)
	}
	return nil
}

// Stop signals the scheduler loop to exit and waits for it to finish.
// In-flight executor goroutines are not awaited — they run to completion
// independently. Idempotent.
func (s *Service) Stop() {
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
	s.mu.Unlock()

	// Wait for the loop to exit so a subsequent Start sees a clean slate.
	if loopDone != nil {
		select {
		case <-loopDone:
		case <-time.After(5 * time.Second):
			s.logger.Warn("cron scheduler loop did not exit within 5s")
		}
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
