package cron

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// --- Job execution (mirrors execute-job.ts, job-result.ts) ---

// triggerSource identifies which path requested a job execution. The
// scheduler loop ("scheduler") and missed-job recovery ("recover") always
// advance NextRunAtMs to the next match. Manual operator runs ("manual")
// preserve a future NextRunAtMs so a manual nudge doesn't accidentally
// skip the next scheduled fire (the bug seen on 2026-04-28 morning-letter).
type triggerSource string

const (
	triggerScheduler triggerSource = "scheduler"
	triggerRecover   triggerSource = "recover"
	triggerManual    triggerSource = "manual"
)

// executeJobFull is the back-compat entry point for callers that don't
// distinguish trigger sources. It treats the run as scheduler-driven.
func (s *Service) executeJobFull(ctx context.Context, job StoreJob) RunOutcome {
	return s.executeJobFullWithTrigger(ctx, job, triggerScheduler)
}

// executeJobFullWithTrigger runs the job once, then consumes a PendingRerun
// flag set while it was running (an overlapping trigger the per-job guard
// dropped). Bounded so a trigger storm cannot loop the executor forever — a
// leftover flag is picked up by the next trigger or the Service.Start boot
// scan. Aborted and skipped outcomes never consume the flag here: aborted
// means the gateway is shutting down (the rerun belongs to the next boot),
// skipped means another executor owns the job (and its own post-run loop).
func (s *Service) executeJobFullWithTrigger(ctx context.Context, job StoreJob, trigger triggerSource) RunOutcome {
	outcome := s.runJobOnce(ctx, job, trigger)
	const maxPendingRerunCycles = 3
	for cycle := 1; cycle <= maxPendingRerunCycles; cycle++ {
		if outcome.Status == "aborted" || outcome.Status == "skipped" || ctx.Err() != nil {
			break
		}
		fresh := s.store.Job(job.ID)
		if fresh == nil || !fresh.State.PendingRerun {
			break
		}
		state := fresh.State
		state.PendingRerun = false
		if err := s.store.UpdateJobState(fresh.ID, state); err != nil {
			s.logger.Warn("cron pending rerun: flag clear failed; leaving for next trigger",
				"id", fresh.ID, "error", err)
			break
		}
		fresh.State = state
		s.logger.Info("cron pending rerun: executing", "id", fresh.ID, "cycle", cycle)
		outcome = s.runJobOnce(ctx, *fresh, trigger)
	}
	return outcome
}

// markPendingRerun persists JobState.PendingRerun=true for the job, so a run
// lost to the overlap guard or a shutdown abort is retried (post-run loop or
// boot scan) instead of silently dropped.
func (s *Service) markPendingRerun(jobID, why string) {
	fresh := s.store.Job(jobID)
	if fresh == nil || fresh.State.PendingRerun {
		return
	}
	state := fresh.State
	state.PendingRerun = true
	if err := s.store.UpdateJobState(jobID, state); err != nil {
		s.logger.Error("cron pending rerun: persist failed — run may be lost",
			"id", jobID, "reason", why, "error", err)
		return
	}
	s.logger.Info("cron pending rerun: queued", "id", jobID, "reason", why)
}

func (s *Service) runJobOnce(ctx context.Context, job StoreJob, trigger triggerSource) RunOutcome {
	if outcome, acquired := s.beginCronJobRun(job); !acquired {
		return outcome
	}
	defer s.runningJobs.Delete(job.ID)

	// Re-load fresh job data from store to avoid stale state from scheduler closures.
	if fresh := s.store.Job(job.ID); fresh != nil {
		job = *fresh
	}
	run := s.newCronJobRun(ctx, job, trigger)
	defer run.close()
	outcome := run.execute()
	run.finalize(outcome)
	return outcome
}

// applyJobResult updates the job state after execution.
// Run-level details (status, error, duration) are stored in the session via session.Manager;
// only cron-specific bookkeeping (consecutive errors, delivery, scheduling) is persisted here.
//
// NextRunAtMs policy by trigger:
//   - scheduler / recover: always advance to next match (job was due now).
//   - manual: preserve a future NextRunAtMs (operator nudge shouldn't skip
//     the next scheduled fire); only advance if the existing NextRunAtMs
//     is in the past or zero.
//
// At the end, signalWake() nudges the scheduler loop so the new
// NextRunAtMs is picked up immediately rather than waiting for the
// loop's next idle tick.
func (s *Service) applyJobResult(job StoreJob, outcome RunOutcome, sessionKey string, trigger triggerSource) {
	state := job.State
	state.LastSessionKey = sessionKey

	// Re-read the live PendingRerun flag: an overlapping trigger may have
	// queued a rerun while this run executed, and `state` above is the
	// pre-run snapshot — persisting it unmerged would clobber the flag.
	if fresh := s.store.Job(job.ID); fresh != nil && fresh.State.PendingRerun {
		state.PendingRerun = true
	}

	switch outcome.Status {
	case "ok":
		state.ConsecutiveErrors = 0
	case "aborted":
		// A shutdown killed the turn mid-run. Queue a rerun for the next boot
		// (Service.Start scan) and leave the error counter alone — restart
		// churn must not walk a healthy job toward auto-disable.
		state.PendingRerun = true
	default:
		state.ConsecutiveErrors++
		if state.ConsecutiveErrors >= 10 {
			state.ScheduleErrorCount++
			if err := s.store.SetJobEnabled(job.ID, false); err != nil {
				s.logger.Error("cron auto-disable persistence failed — job may re-fail",
					"id", job.ID, "error", err)
			}
			state.AutoDisabledAtMs = time.Now().UnixMilli()
			s.logger.Warn("cron job auto-disabled after consecutive errors",
				"id", job.ID, "consecutiveErrors", state.ConsecutiveErrors)
			s.emit(CronEvent{Type: "job_auto_disabled", JobID: job.ID})
		}
	}

	if outcome.Delivery != nil {
		if outcome.Delivery.Delivered {
			state.LastDeliveryStatus = "delivered"
			state.LastDeliveryError = ""
		} else {
			state.LastDeliveryStatus = "not-delivered"
			state.LastDeliveryError = outcome.Delivery.Error
		}
	}

	nowMs := time.Now().UnixMilli()
	before := job.State.NextRunAtMs

	// Trigger-aware NextRunAtMs policy. For manual runs, preserve a future
	// NextRunAtMs so the next scheduled fire isn't accidentally skipped.
	preserved := false
	if trigger == triggerManual && before > nowMs {
		state.NextRunAtMs = before
		preserved = true
	} else {
		state.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, nowMs)
	}

	persistErr := s.store.UpdateJobState(job.ID, state)
	s.logger.Info("cron scheduler decision",
		"action", "applyJobResult",
		"reason", "executorFinish",
		"trigger", string(trigger),
		"preservedNextRun", preserved,
		"id", job.ID,
		"sessionKey", sessionKey,
		"status", outcome.Status,
		"beforeNextRunAtMs", before,
		"afterNextRunAtMs", state.NextRunAtMs,
		"nowMs", nowMs,
		"persistErr", errStr(persistErr))
	if persistErr != nil {
		s.logger.Error("cron job state persist failed — next schedule may be wrong",
			"id", job.ID, "error", persistErr)
	}

	// Wake the scheduler loop so it re-evaluates with the new NextRunAtMs.
	// Without this, a job whose new NextRunAtMs is sooner than the loop's
	// current sleep target wouldn't fire on time. signalWake is non-blocking
	// and safe to call even when the loop isn't running yet.
	s.signalWake()
}

// ShouldSendFailureAlert checks if a failure alert should be sent for a job.
// Respects cooldown period and error status.
func ShouldSendFailureAlert(state JobState, failureAlert *CronFailureAlert, outcomeStatus string, nowMs int64) bool {
	if outcomeStatus != "error" {
		return false
	}
	if failureAlert == nil {
		return false
	}
	// Respect "after" threshold.
	if failureAlert.After > 0 && state.ConsecutiveErrors < failureAlert.After {
		return false
	}
	// Respect cooldown.
	if failureAlert.CooldownMs > 0 && state.LastFailureAlertAtMs > 0 {
		if nowMs-state.LastFailureAlertAtMs < failureAlert.CooldownMs {
			return false
		}
	}
	return true
}

// koreanFailureCause maps a RunOutcome.Error (raw, often-English internal
// strings like "job already running" or "delivery target error: …") to a short
// Korean phrase for the user-facing failure alert. The raw string stays in the
// operator log (see sendFailureAlert); the push gets a clean, in-persona cause.
func koreanFailureCause(raw string) string {
	switch {
	case raw == "":
		return "원인 미상"
	case strings.Contains(raw, "already running"),
		strings.Contains(raw, "concurrent execution"):
		return "이미 실행 중이어서 건너뜀"
	case strings.Contains(raw, "delivery target"):
		return "결과 전달 실패"
	case strings.Contains(raw, "no agent runner"):
		return "실행기가 구성되지 않음"
	case strings.Contains(raw, "context deadline"),
		strings.Contains(raw, "timeout"):
		return "시간 초과"
	case strings.Contains(raw, "connection refused"):
		return "백엔드 연결 실패"
	}
	return "내부 오류"
}

// sendFailureAlert delivers a failure notification for a cron job.
func (s *Service) sendFailureAlert(ctx context.Context, job StoreJob, outcome RunOutcome) {
	alert := job.FailureAlert
	ch := alert.Channel
	if ch == "" {
		ch = s.cfg.DefaultChannel
	}
	to := alert.To
	if to == "" {
		to = s.cfg.DefaultTo
	}
	if ch == "" || to == "" {
		s.logger.Warn("failure alert skipped: no channel/to", "jobID", job.ID)
		return
	}

	cause := koreanFailureCause(outcome.Error)
	if cause == "내부 오류" {
		// Unrecognized cause — keep the raw string in the operator log so the
		// generic Korean phrase in the push doesn't hide it.
		s.logger.Warn("cron failure: unmapped cause", "jobID", job.ID, "raw", outcome.Error)
	}
	text := fmt.Sprintf("⚠️ 크론 작업 '%s' 실행 실패 (연속 %d회): %s", job.Name, job.State.ConsecutiveErrors, cause)

	// Deliver via the main-session handoff (native client 업무 transcript +
	// push), the same path regular cron output uses. ch/to are passed for the
	// legacy relay signature but ignored in native-only mode.
	if s.cfg.MainSessionHandoff == nil {
		s.logger.Error("failure alert delivery failed", "jobID", job.ID, "error", "main session handoff not configured")
		return
	}
	handled, err := s.cfg.MainSessionHandoff(ctx, ch, to, job.ID, text)
	if err != nil || !handled {
		s.logger.Error("failure alert delivery failed",
			"jobID", job.ID, "handled", handled, "error", err)
		// Do NOT start the cooldown on an alert the operator never received.
		// The cooldown exists to stop DUPLICATE alerts; arming it after a failed
		// delivery converts a broken handoff into total silence — the job keeps
		// failing, every later alert is suppressed by a window opened for a
		// message nobody got, and the only trace is this log line. The next
		// failed run retries the alert instead.
		return
	}

	// Update last failure alert timestamp so the cooldown window works.
	// Persist failure — if this write fails we'd spam the user with duplicate
	// alerts on every subsequent failed run.
	state := job.State
	state.LastFailureAlertAtMs = time.Now().UnixMilli()
	if err := s.store.UpdateJobState(job.ID, state); err != nil {
		s.logger.Error("cron failure-alert timestamp persist failed — may duplicate alerts",
			"id", job.ID, "error", err)
	}
}

func resolveJobCommand(job StoreJob) string {
	if job.Payload.Kind == "systemEvent" {
		return job.Payload.Text
	}
	return job.Payload.Message
}

func isBestEffort(cfg *JobDeliveryConfig) bool {
	return cfg != nil && cfg.BestEffort
}

func safeStr(target *DeliveryTarget, fn func(*DeliveryTarget) string) string {
	if target == nil {
		return ""
	}
	return fn(target)
}
