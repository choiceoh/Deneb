package cron

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Scheduler architecture (rewrite, 2026-04-28)
//
// Previous design: a chain of single-shot timers where each fire spawned the
// next timer. Any of these conditions silently killed the chain forever:
//   - store.Load() returning a transient error
//   - all enabled jobs being temporarily in runningJobs at arm-time
//   - any panic in fire/arm (no panic recovery on the timer goroutine)
//   - applyJobResult / Run / Wake forgetting to re-arm
// Recovery required restart, Add(), or Update().
//
// New design: one long-lived worker goroutine (panic-recovered via safego)
// that loops forever. It sleeps until either:
//   - a wake signal arrives on s.wakeCh (sent by Add/Update/Remove/Run/Wake
//     /applyJobResult/Start to indicate state changed and the next-wake
//     computation should re-run), or
//   - the computed sleep duration elapses (next-due time, idle fallback,
//     or error backoff).
// On wake, it fires all due jobs and recomputes the next sleep. The loop
// never exits on transient errors — it backs off and retries. The chain
// can no longer "die".
//
// Wake channel is buffered with cap=1 and uses non-blocking sends, so
// callers never block and duplicate wakes coalesce into one work cycle.

// Tunable timing constants for the scheduler loop.
const (
	// idleInterval is the maximum sleep when there are no future-due jobs.
	// The loop wakes at least this often so newly-enabled jobs and clock
	// adjustments are picked up without an explicit wake signal.
	defaultIdleInterval = 60 * time.Second

	// errBackoff is how long the loop sleeps after a store-load failure
	// before retrying. Bounded so transient I/O glitches don't busy-loop.
	defaultErrBackoff = 5 * time.Second

	// minLoopGap caps how fast the loop can spin, even if every iteration
	// requests an immediate wake. Prevents CPU runaway from misbehaving
	// schedules (NextRunAtMs always in the past, etc).
	defaultMinLoopGap = 100 * time.Millisecond
)

// signalWake non-blockingly notifies the scheduler loop that job state
// changed and the next-wake computation should re-run. Safe to call from
// any goroutine; safe to call when the loop isn't running (drops on the
// floor in that case). Callers should invoke this after Add/Update/Remove
// /enable-toggle/manual-run/recover so the loop sees the new state
// immediately rather than waiting for its current sleep to elapse.
func (s *Service) signalWake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
		// Channel already has a pending wake; coalesce.
	}
}

// runSchedulerLoop is the single long-lived worker goroutine. Spawned by
// Start; exits only when ctx is cancelled or Stop closes loopDone.
func (s *Service) runSchedulerLoop(ctx context.Context) {
	defer close(s.loopDone)

	for {
		// Compute how long to sleep before the next wake.
		sleep := s.computeSleep()

		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-s.wakeCh:
			// State changed — recompute and fire any due jobs.
		case <-time.After(sleep):
			// Sleep expired — fire any due jobs.
		}

		// Brief minimum gap to prevent runaway loops on degenerate state.
		if s.minLoopGap > 0 {
			time.Sleep(s.minLoopGap)
		}

		s.tickLocked(ctx)
	}
}

// computeSleep returns how long the worker should sleep before its next
// wake. Bounded by [minLoopGap, idleInterval]. On store-load error,
// returns errBackoff so the loop retries without spinning.
func (s *Service) computeSleep() time.Duration {
	storeData, err := s.store.Load()
	if err != nil {
		s.logger.Warn("cron scheduler store load failed; backing off",
			"error", err, "backoff", s.errBackoff)
		return s.errBackoff
	}
	if storeData == nil {
		return s.idleInterval
	}

	now := time.Now().UnixMilli()
	var nextWakeMs int64

	s.mu.Lock()
	for _, job := range storeData.Jobs {
		if !job.Enabled || job.State.NextRunAtMs <= 0 {
			continue
		}
		// Jobs currently executing don't influence next-wake — their
		// NextRunAtMs hasn't been advanced by applyJobResult yet, and
		// the worker will get a wake signal when they finish.
		if _, running := s.runningJobs.Load(job.ID); running {
			continue
		}
		if nextWakeMs == 0 || job.State.NextRunAtMs < nextWakeMs {
			nextWakeMs = job.State.NextRunAtMs
		}
	}
	s.nextWakeAtMs = nextWakeMs
	s.mu.Unlock()

	if nextWakeMs == 0 {
		return s.idleInterval
	}
	delay := time.Duration(nextWakeMs-now) * time.Millisecond
	if delay < s.minLoopGap {
		return s.minLoopGap
	}
	if delay > s.idleInterval {
		return s.idleInterval
	}
	return delay
}

// tickLocked runs one fire pass: load store, find due jobs, spawn executors.
// Holds s.mu while iterating + pre-advancing, then releases before the
// executor goroutines run (they take s.mu again later via applyJobResult).
func (s *Service) tickLocked(ctx context.Context) {
	storeData, err := s.store.Load()
	if err != nil {
		s.logger.Warn("cron scheduler tick store load failed", "error", err)
		return
	}
	if storeData == nil {
		return
	}

	now := time.Now().UnixMilli()

	s.mu.Lock()
	var toRun []StoreJob
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs <= 0 || job.State.NextRunAtMs > now {
			continue
		}
		if _, running := s.runningJobs.Load(job.ID); running {
			continue
		}
		// Pre-advance NextRunAtMs in the store before spawning the
		// executor so a concurrent tick path (e.g. a wake arriving
		// during executor startup) doesn't observe the same overdue
		// NextRunAtMs and double-fire. applyJobResult overwrites with
		// the definitive post-run value.
		preAdvanceNextRun(s, job, now, "fire")
		toRun = append(toRun, job)
	}
	s.mu.Unlock()

	for _, job := range toRun {
		jobCopy := job
		// Each executor runs in its own panic-recovered goroutine.
		// applyJobResult will signalWake() when it finishes so the
		// loop re-evaluates the next-wake without waiting for sleep.
		safego.GoWithSlog(s.logger, "cron-executor", func() {
			s.executeJobFull(ctx, jobCopy)
		})
	}
}

// collectMissedJobsLocked returns jobs that should have fired during
// downtime, after pre-advancing their NextRunAtMs. The caller is expected
// to spawn executors for each returned job AFTER releasing s.mu, so the
// executor goroutines never race with code holding s.mu.
//
// Pre-advance is done while holding s.mu so a fresh store.Load() in the
// scheduler loop's first tick won't see these jobs as still-due.
func (s *Service) collectMissedJobsLocked(storeData *CronStoreFile) []StoreJob {
	now := time.Now().UnixMilli()
	var toRecover []StoreJob
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now {
			if _, running := s.runningJobs.Load(job.ID); running {
				continue
			}
			preAdvanceNextRun(s, job, now, "recover")
			s.logger.Info("recovering missed cron job", "id", job.ID,
				"scheduledAt", job.State.NextRunAtMs, "missedBy", now-job.State.NextRunAtMs)
			toRecover = append(toRecover, job)
		}
	}
	return toRecover
}

// spawnRecoverExecutors spawns one panic-recovered executor goroutine per
// missed job. Must be called WITHOUT s.mu held.
func (s *Service) spawnRecoverExecutors(ctx context.Context, jobs []StoreJob) {
	for _, job := range jobs {
		jobCopy := job
		safego.GoWithSlog(s.logger, "cron-executor-recover", func() {
			s.executeJobFullWithTrigger(ctx, jobCopy, triggerRecover)
		})
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
