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
			// Eagerly advance NextRunAtMs in the store *before* spawning the
			// executor. Without this, a second scheduler path (timer rearm,
			// Wake, or recoverMissedJobs) can re-observe the same overdue
			// NextRunAtMs and spawn a second executor for the same trigger.
			// The executor's applyJobResult still recomputes NextRunAtMs based
			// on the post-run time, so this pre-advance is idempotent.
			preAdvanceNextRun(s, job, now, "fire")
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
			if _, running := s.runningJobs.Load(job.ID); running {
				continue
			}
			// Pre-advance NextRunAtMs so a subsequent timer fire (armed
			// immediately after recover) won't treat this job as still due.
			preAdvanceNextRun(s, job, now, "recover")
			s.logger.Info("recovering missed cron job", "id", job.ID,
				"scheduledAt", job.State.NextRunAtMs, "missedBy", now-job.State.NextRunAtMs)
			jobCopy := job
			go func() {
				s.executeJobFull(ctx, jobCopy)
			}()
		}
	}
}

// preAdvanceNextRun writes the next scheduled time to disk before the executor
// starts, so a concurrent scheduler path sees the job as not-due and skips it.
// Errors are logged only — the executor will still run and applyJobResult
// overwrites with the definitive post-run NextRunAtMs.
//
// reason is a short tag ("fire", "recover") identifying the caller so a
// postmortem can tell which scheduler path decided to advance the clock.
func preAdvanceNextRun(s *Service, job StoreJob, now int64, reason string) {
	nextMs := ComputeNextRunAtMs(job.Schedule, now)
	degenerate := false
	if nextMs <= now {
		// Degenerate schedule (should not happen); fall back to +1 minute
		// to at least keep the window closed for a tick.
		nextMs = now + 60_000
		degenerate = true
	}
	updatedState := job.State
	updatedState.NextRunAtMs = nextMs
	persistErr := s.store.UpdateJobState(job.ID, updatedState)
	// One log per advance so the reader can grep `cron scheduler decision
	// id=X` and see every NextRunAtMs mutation + who made it. This was the
	// missing piece when diagnosing the 4/24 19:00 cron miss — scheduler
	// state changed but the trail was empty.
	s.logger.Info("cron scheduler decision",
		"action", "preAdvanceNextRun",
		"reason", reason,
		"id", job.ID,
		"beforeNextRunAtMs", job.State.NextRunAtMs,
		"afterNextRunAtMs", nextMs,
		"nowMs", now,
		"degenerate", degenerate,
		"persistErr", errStr(persistErr))
	if persistErr != nil {
		s.logger.Warn("cron pre-advance NextRunAtMs failed",
			"id", job.ID, "error", persistErr)
	}
}

// errStr renders an error as a string or empty — convenient for slog key-value
// pairs where a nil error should render as "".
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
