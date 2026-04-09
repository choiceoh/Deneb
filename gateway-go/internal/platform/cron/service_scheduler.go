package cron

import (
	"context"
	"time"
)

// --- Timer management ---
// A single timer wakes at the earliest NextRunAtMs across all jobs.
// On fire, it executes due jobs and re-arms for the next wake.

func (s *Service) armTimerLocked(ctx context.Context) {
	s.disarmTimerLocked()

	// Compute next wake time across all jobs.
	storeData, err := s.store.Load()
	if err != nil {
		s.logger.Warn("failed to load cron store for timer arm", "error", err)
		return
	}
	if storeData == nil {
		return
	}

	now := time.Now().UnixMilli()
	var nextWake int64
	for _, job := range storeData.Jobs {
		if !job.Enabled || job.State.NextRunAtMs <= 0 {
			continue
		}
		// Skip jobs that are currently running — their NextRunAtMs hasn't
		// been advanced yet, so including them would cause the timer to
		// re-fire every minRefireGap just to hit the concurrency guard.
		if _, running := s.runningJobs.Load(job.ID); running {
			continue
		}
		if nextWake == 0 || job.State.NextRunAtMs < nextWake {
			nextWake = job.State.NextRunAtMs
		}
	}

	if nextWake == 0 {
		s.nextWakeAtMs = 0
		return
	}

	delayMs := nextWake - now
	if delayMs < s.minRefireGap {
		delayMs = s.minRefireGap
	}
	if delayMs > s.maxTimerDelay {
		delayMs = s.maxTimerDelay
	}

	s.nextWakeAtMs = now + delayMs

	timerCtx, cancel := context.WithCancel(ctx)
	s.timerCancel = cancel

	go func() {
		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timerCtx.Done():
			return
		case <-timer.C:
			s.mu.Lock()
			s.fireTimerLocked(ctx)
			s.armTimerLocked(ctx)
			s.mu.Unlock()
		}
	}()
}

func (s *Service) disarmTimerLocked() {
	if s.timerCancel != nil {
		s.timerCancel()
		s.timerCancel = nil
	}
	s.nextWakeAtMs = 0
}

func (s *Service) fireTimerLocked(ctx context.Context) {
	now := time.Now().UnixMilli()

	// Enforce minimum refire gap.
	if now-s.lastFireAtMs < s.minRefireGap {
		return
	}
	s.lastFireAtMs = now

	// Find and run due jobs.
	storeData, err := s.store.Load()
	if err != nil {
		s.logger.Warn("failed to load cron store for timer fire", "error", err)
		return
	}
	if storeData == nil {
		return
	}

	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now {
			// Skip jobs already running to avoid goroutine + log churn.
			if _, running := s.runningJobs.Load(job.ID); running {
				continue
			}
			// Execute asynchronously.
			jobCopy := job
			go func() {
				s.executeJobFull(ctx, jobCopy)
			}()
		}
	}
}

// recoverMissedJobsLocked runs jobs that should have fired during downtime.
func (s *Service) recoverMissedJobsLocked(ctx context.Context, storeData *CronStoreFile) {
	now := time.Now().UnixMilli()
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now {
			s.logger.Info("recovering missed cron job", "id", job.ID,
				"scheduledAt", job.State.NextRunAtMs, "missedBy", now-job.State.NextRunAtMs)
			jobCopy := job
			go func() {
				s.executeJobFull(ctx, jobCopy)
			}()
		}
	}
}
