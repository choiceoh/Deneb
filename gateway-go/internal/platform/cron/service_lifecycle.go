package cron

import (
	"context"
	"fmt"
	"time"
)

// Start loads jobs from the store and begins scheduling.
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

	// Initialize scheduling for all enabled jobs.
	// One-shot "at" jobs use the scheduler for immediate execution.
	// Recurring "every"/"cron" jobs rely on the timer via NextRunAtMs.
	scheduled := 0
	nowMs := time.Now().UnixMilli()
	for i := range storeData.Jobs {
		job := &storeData.Jobs[i]
		if !job.Enabled {
			continue
		}
		if job.Schedule.Kind == "at" {
			// One-shot jobs: register with scheduler for immediate execution.
			if err := s.scheduleJobLocked(ctx, *job); err != nil {
				s.logger.Warn("failed to schedule cron job", "id", job.ID, "error", err)
				continue
			}
		} else {
			// Recurring jobs: ensure NextRunAtMs is set so the timer can fire them.
			if job.State.NextRunAtMs <= 0 {
				job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, nowMs)
				if job.State.NextRunAtMs > 0 {
					s.store.UpdateJobState(job.ID, job.State) //nolint:errcheck // best-effort
				}
			}
		}
		scheduled++
	}

	// Check for missed jobs (jobs that should have fired during downtime).
	s.recoverMissedJobsLocked(ctx, storeData)

	// Arm the next-wake timer for recurring jobs.
	s.armTimerLocked(ctx)

	s.running = true
	if scheduled > 0 {
		s.logger.Info("cron service started", "scheduled", scheduled)
	}
	return nil
}

// Stop cancels all scheduled jobs and the timer.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.scheduler.Close()
	s.disarmTimerLocked()
	s.running = false
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.logger.Info("cron service stopped")
}

// Status returns the service status.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	running := s.running
	nextWake := s.nextWakeAtMs
	s.mu.Unlock()

	tasks := s.scheduler.List()
	return ServiceStatus{
		Running:     running,
		TaskCount:   len(tasks),
		NextRunAtMs: nextWake,
		Tasks:       tasks,
	}
}
