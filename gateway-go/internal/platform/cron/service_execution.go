package cron

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
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
	// Per-job execution guard: skip if this job is already running.
	if _, loaded := s.runningJobs.LoadOrStore(job.ID, true); loaded {
		s.logger.Info("cron job already running, skipping", "id", job.ID)
		s.runLog.Append(RunLogEntry{ //nolint:errcheck // best-effort
			Ts:     time.Now().UnixMilli(),
			JobID:  job.ID,
			Action: "skipped",
			Status: "skipped",
			Error:  "concurrent execution prevented",
		})
		// The dropped trigger is otherwise lost — for trigger-per-event jobs
		// (mail watch) that means the event's analysis never happens. Queue a
		// rerun for the owning executor's post-run consumption loop.
		s.markPendingRerun(job.ID, "overlap trigger skipped")
		return RunOutcome{
			Status:    "skipped",
			Error:     "job already running",
			StartedAt: time.Now().UnixMilli(),
			EndedAt:   time.Now().UnixMilli(),
		}
	}
	defer s.runningJobs.Delete(job.ID)

	// Re-load fresh job data from store to avoid stale state from scheduler closures.
	if fresh := s.store.Job(job.ID); fresh != nil {
		job = *fresh
	}

	startedAt := time.Now().UnixMilli()
	s.emit(CronEvent{Type: "job_started", JobID: job.ID})

	// Apply timeout (mirrors timeout-policy.ts).
	timeoutMs := ResolveCronJobTimeoutMs(job)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Resolve delivery target.
	deliveryCfg := job.Delivery
	target, targetErr := ResolveDeliveryTarget(deliveryCfg, s.cfg.DefaultChannel, s.cfg.DefaultTo)

	// Build agent turn params.
	command := resolveJobCommand(job)
	sessionKey := fmt.Sprintf("cron:%s:%d", job.ID, startedAt)

	var outcome RunOutcome

	if targetErr != nil && !isBestEffort(deliveryCfg) { //nolint:gocritic // ifElseChain — complex branching
		outcome = RunOutcome{
			Status:     "error",
			Error:      fmt.Sprintf("delivery target error: %s", targetErr),
			StartedAt:  startedAt,
			EndedAt:    time.Now().UnixMilli(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	} else if s.agent != nil {
		// Create cron run session in session.Manager if available.
		sessionKind := session.KindCron
		if job.SessionTarget == SessionTargetSubagent && s.cfg.TranscriptCloner != nil && s.cfg.MainSessionKey != "" {
			if err := s.cfg.TranscriptCloner.CloneRecent(s.cfg.MainSessionKey, sessionKey, 20); err != nil {
				s.logger.Warn("cron session transcript clone failed", "jobId", job.ID, "error", err)
			}
		}
		if s.cfg.Sessions != nil {
			s.cfg.Sessions.Create(sessionKey, sessionKind)
		}

		// Retry loop: attempt up to retryCount+1 times with exponential backoff.
		maxRetries := job.Payload.RetryCount
		if maxRetries < 0 {
			maxRetries = 0
		}
		if maxRetries > 3 {
			maxRetries = 3
		}
		retryBackoff := job.Payload.RetryBackoffMs
		if retryBackoff <= 0 {
			retryBackoff = 5000
		}

		var output string
		var runErr error
		retriesUsed := 0

		for attempt := 0; attempt <= maxRetries; attempt++ {
			output, runErr = s.agent.RunAgentTurn(runCtx, AgentTurnParams{
				SessionKey: sessionKey,
				AgentID:    job.AgentID,
				Command:    command,
				Channel:    safeStr(target, func(t *DeliveryTarget) string { return t.Channel }),
				To:         safeStr(target, func(t *DeliveryTarget) string { return t.To }),
				AccountID:  safeStr(target, func(t *DeliveryTarget) string { return t.AccountID }),
				ThreadID:   safeStr(target, func(t *DeliveryTarget) string { return t.ThreadID }),
				Thinking:   job.Payload.Thinking,
			})
			if runErr == nil {
				break
			}
			// Don't retry on context cancellation/timeout.
			if runCtx.Err() != nil {
				break
			}
			if attempt < maxRetries {
				retriesUsed++
				backoff := time.Duration(retryBackoff<<uint(attempt)) * time.Millisecond
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
				s.logger.Info("cron job retrying", "id", job.ID, "attempt", attempt+2, "of", maxRetries+1, "backoff", backoff)
				select {
				case <-runCtx.Done():
					runErr = runCtx.Err()
				case <-time.After(backoff):
				}
				if runCtx.Err() != nil {
					break
				}
			}
		}

		if runErr != nil {
			status := "error"
			errMsg := runErr.Error()
			if errors.Is(runErr, ErrTurnAborted) {
				// Infra churn (a restart killed the turn), not a job fault:
				// applyJobResult queues a rerun and skips the error counter so
				// a deploy storm cannot auto-disable a healthy job.
				status = "aborted"
			} else if runCtx.Err() == context.DeadlineExceeded {
				status = "timeout"
				elapsed := time.Duration(time.Now().UnixMilli()-startedAt) * time.Millisecond
				timeoutDur := time.Duration(timeoutMs) * time.Millisecond
				errMsg = fmt.Sprintf("timeout after %s (limit: %s)", elapsed.Round(time.Second), timeoutDur.Round(time.Second))
			}
			outcome = RunOutcome{
				Status:     status,
				Error:      errMsg,
				Retries:    retriesUsed,
				StartedAt:  startedAt,
				EndedAt:    time.Now().UnixMilli(),
				DurationMs: time.Now().UnixMilli() - startedAt,
			}
		} else {
			// Wait for descendant subagents if the output looks like an interim ack.
			output = pollSubagentOutputs(runCtx, s.cfg.SubagentPoller, sessionKey, output)

			// Deliver output to target channel.
			//
			// Preferred path: hand the analysis off to the main user session
			// so the main agent is the literal sender and the main session
			// transcript records the proactive turn (see
			// ServiceConfig.MainSessionHandoff). Falls back to direct
			// delivery if no handoff is configured, the handoff declines
			// (handled=false), or the handoff errors.
			var deliveryResult *DeliveryResult
			if output != "" && target != nil {
				stripped := tokens.StripHeartbeatToken(output, tokens.StripModeHeartbeat, tokens.DefaultHeartbeatAckChars)
				if !stripped.ShouldSkip {
					deliveryText := output
					if stripped.DidStrip && stripped.Text != "" {
						deliveryText = stripped.Text
					}

					if s.cfg.MainSessionHandoff != nil {
						handled, herr := s.cfg.MainSessionHandoff(runCtx, target.Channel, target.To, job.ID, deliveryText)
						switch {
						case herr != nil:
							// A real delivery failure — the relay errored (e.g. the
							// transcript append failed). Record it as not-delivered so
							// the promote-to-error path below fires: status becomes
							// "error", consecutive failures count toward auto-disable,
							// and a failure alert can go out. Without this the run was
							// logged "ok" and the user silently lost the report.
							// A bare handled=false with no error is an intentional
							// suppression (NO_REPLY / the "nothing to report" noise
							// floor in proactive_relay.go) and correctly stays "ok".
							s.logger.Warn("cron main-session handoff failed",
								"jobId", job.ID,
								"channel", target.Channel,
								"to", target.To,
								"error", herr)
							deliveryResult = &DeliveryResult{
								Delivered: false,
								Channel:   target.Channel,
								To:        target.To,
								Error:     herr.Error(),
							}
						case handled:
							// Main session delivered to the user. Record
							// delivery as successful from cron's point of
							// view — the main session owns retry/visibility
							// from here.
							deliveryResult = &DeliveryResult{
								Delivered: true,
								Channel:   target.Channel,
								To:        target.To,
							}
						}
					}
				}
			}

			// Promote to error status when a required delivery failed.
			// Without this, consecutive delivery failures never trigger the
			// auto-disable path (10 errors → job disabled) and the user
			// keeps losing cron output silently.
			status := "ok"
			errMsg := ""
			if deliveryResult != nil && !deliveryResult.Delivered && !isBestEffort(deliveryCfg) {
				status = "error"
				errMsg = "delivery failed: " + deliveryResult.Error
			}
			outcome = RunOutcome{
				Status:     status,
				Error:      errMsg,
				Output:     output,
				Delivery:   deliveryResult,
				Retries:    retriesUsed,
				StartedAt:  startedAt,
				EndedAt:    time.Now().UnixMilli(),
				DurationMs: time.Now().UnixMilli() - startedAt,
			}
		}
	} else {
		outcome = RunOutcome{
			Status:     "error",
			Error:      "no agent runner configured",
			StartedAt:  startedAt,
			EndedAt:    time.Now().UnixMilli(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// Apply result to job state; run-level details live in the session.
	s.applyJobResult(job, outcome, sessionKey, trigger)

	// Send failure alert if configured and conditions are met. Delivery goes
	// through the main-session handoff (native client), so it requires that
	// callback to be wired rather than a Telegram plugin.
	if s.cfg.MainSessionHandoff != nil && ShouldSendFailureAlert(job.State, job.FailureAlert, outcome.Status, time.Now().UnixMilli()) {
		s.sendFailureAlert(ctx, job, outcome)
	}

	// Log run with delivery status and retry count.
	logEntry := RunLogEntry{
		Ts:          time.Now().UnixMilli(),
		JobID:       job.ID,
		Action:      "finished",
		Status:      outcome.Status,
		Error:       outcome.Error,
		Summary:     PickSummaryFromOutput(outcome.Output),
		DurationMs:  outcome.DurationMs,
		NextRunAtMs: ComputeNextRunAtMs(job.Schedule, time.Now().UnixMilli()),
		Retries:     outcome.Retries,
	}
	if outcome.Delivery != nil {
		logEntry.Delivered = outcome.Delivery.Delivered
		if outcome.Delivery.Delivered {
			logEntry.DeliveryStatus = "delivered"
		} else {
			logEntry.DeliveryStatus = "not-delivered"
			logEntry.DeliveryError = outcome.Delivery.Error
		}
	}
	s.runLog.Append(logEntry) //nolint:errcheck // best-effort

	s.emit(CronEvent{Type: "job_finished", JobID: job.ID, Status: outcome.Status})
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
	handled, err := s.cfg.MainSessionHandoff(ctx, ch, to, job.ID, text)
	if err != nil || !handled {
		s.logger.Error("failure alert delivery failed",
			"jobID", job.ID, "handled", handled, "error", err)
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
