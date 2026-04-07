package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// --- Job execution (mirrors execute-job.ts, job-result.ts) ---

func (s *Service) executeJobFull(ctx context.Context, job StoreJob) RunOutcome {
	// Per-job execution guard: skip if this job is already running.
	if _, loaded := s.runningJobs.LoadOrStore(job.ID, true); loaded {
		s.logger.Info("cron job already running, skipping", "id", job.ID)
		s.runLog.Append(RunLogEntry{
			Ts:     time.Now().UnixMilli(),
			JobID:  job.ID,
			Action: "skipped",
			Status: "skipped",
			Error:  "concurrent execution prevented",
		})
		return RunOutcome{
			Status:    "skipped",
			Error:     "job already running",
			StartedAt: time.Now().UnixMilli(),
			EndedAt:   time.Now().UnixMilli(),
		}
	}
	defer s.runningJobs.Delete(job.ID)

	// Re-load fresh job data from store to avoid stale state from scheduler closures.
	if fresh := s.store.GetJob(job.ID); fresh != nil {
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

	if targetErr != nil && !isBestEffort(deliveryCfg) {
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
			if runCtx.Err() == context.DeadlineExceeded {
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
			// Deliver output to target channel.
			var deliveryResult *DeliveryResult
			if output != "" && target != nil && s.cfg.TelegramPlugin != nil {
				stripped := tokens.StripHeartbeatToken(output, tokens.StripModeHeartbeat, tokens.DefaultHeartbeatAckChars)
				if !stripped.ShouldSkip {
					deliveryText := output
					if stripped.DidStrip && stripped.Text != "" {
						deliveryText = stripped.Text
					}
					payloads := []types.ReplyPayload{{Text: deliveryText}}
					bestEffort := isBestEffort(deliveryCfg)
					dr := DeliverCronOutput(runCtx, s.cfg.TelegramPlugin, *target, payloads, DeliverOutputOptions{
						ChunkLimit: chunk.DefaultLimit,
						ChunkMode:  "length",
						BestEffort: bestEffort,
						Logger:     s.logger,
					})
					deliveryResult = &dr
				}
			}

			outcome = RunOutcome{
				Status:     "ok",
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
	s.applyJobResult(job, outcome, sessionKey)

	// Send failure alert if configured and conditions are met.
	if s.cfg.TelegramPlugin != nil && ShouldSendFailureAlert(job.State, job.FailureAlert, outcome.Status, time.Now().UnixMilli()) {
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
	s.runLog.Append(logEntry)

	s.emit(CronEvent{Type: "job_finished", JobID: job.ID, Status: outcome.Status})
	return outcome
}

// applyJobResult updates the job state after execution.
// Run-level details (status, error, duration) are stored in the session via session.Manager;
// only cron-specific bookkeeping (consecutive errors, delivery, scheduling) is persisted here.
func (s *Service) applyJobResult(job StoreJob, outcome RunOutcome, sessionKey string) {
	state := job.State
	state.LastSessionKey = sessionKey

	if outcome.Status == "ok" {
		state.ConsecutiveErrors = 0
	} else {
		state.ConsecutiveErrors++
		if state.ConsecutiveErrors >= 10 {
			state.ScheduleErrorCount++
			// Auto-disable after 10 consecutive errors.
			s.store.SetJobEnabled(job.ID, false)
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
	state.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, nowMs)

	s.store.UpdateJobState(job.ID, state)
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

	text := fmt.Sprintf("⚠️ 크론 작업 '%s' 실행 실패 (연속 %d회): %s", job.Name, job.State.ConsecutiveErrors, outcome.Error)
	target := DeliveryTarget{Channel: ch, To: to, AccountID: alert.AccountID}
	payloads := []types.ReplyPayload{{Text: text}}

	dr := DeliverCronOutput(ctx, s.cfg.TelegramPlugin, target, payloads, DeliverOutputOptions{
		BestEffort: true,
		Logger:     s.logger,
	})
	if !dr.Delivered {
		s.logger.Warn("failure alert delivery failed", "jobID", job.ID, "error", dr.Error)
	}

	// Update last failure alert timestamp.
	state := job.State
	state.LastFailureAlertAtMs = time.Now().UnixMilli()
	s.store.UpdateJobState(job.ID, state)
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
