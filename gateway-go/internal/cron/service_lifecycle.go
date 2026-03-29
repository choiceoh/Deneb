package cron

import (
	"context"
	"fmt"
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

	// Schedule all enabled jobs.
	scheduled := 0
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if err := s.scheduleJobLocked(ctx, job); err != nil {
			s.logger.Warn("failed to schedule cron job", "id", job.ID, "error", err)
			continue
		}
		scheduled++
	}

	// Check for missed jobs (jobs that should have fired during downtime).
	s.recoverMissedJobsLocked(ctx, storeData)

	// Arm the next-wake timer.
	s.armTimerLocked(ctx)

	s.running = true
	s.logger.Info("cron service started", "total", len(storeData.Jobs), "scheduled", scheduled)
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
