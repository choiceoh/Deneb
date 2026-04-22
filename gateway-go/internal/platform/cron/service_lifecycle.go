package cron

import (
	"context"
	"fmt"
	"time"
)

// Start loads jobs from the store and arms the timer for all enabled jobs.
// One-shot "at" jobs, recurring "every" jobs, and "cron" expression jobs
// all go through the same timer-based system via NextRunAtMs.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	storeData, err := s.store.Load()
	if err != nil {
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
					// Boot-time initialization failure — job still schedules in
					// memory, but disk is stale. Log so the issue surfaces.
					s.logger.Warn("cron boot state init persist failed",
						"id", job.ID, "error", err)
				}
			}
		}
		scheduled++
	}

	// Run jobs that should have fired during downtime.
	s.recoverMissedJobsLocked(ctx, storeData)

	// Arm the single timer for the earliest due job.
	s.armTimerLocked(ctx)

	s.running = true
	if scheduled > 0 {
		s.logger.Info("cron service started", "scheduled", scheduled)
	}
	return nil
}

// Stop cancels the timer and marks the service as stopped.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.disarmTimerLocked()
	s.running = false
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
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
