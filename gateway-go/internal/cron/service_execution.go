package cron

import (
	"context"
	"fmt"
	"time"
)

// --- Job execution (mirrors execute-job.ts, job-result.ts) ---

func (s *Service) executeJobFull(ctx context.Context, job StoreJob) RunOutcome {
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
		output, runErr := s.agent.RunAgentTurn(runCtx, AgentTurnParams{
			SessionKey: fmt.Sprintf("cron:%s:%d", job.ID, startedAt),
			AgentID:    job.AgentID,
			Command:    command,
			Channel:    safeStr(target, func(t *DeliveryTarget) string { return t.Channel }),
			To:         safeStr(target, func(t *DeliveryTarget) string { return t.To }),
			AccountID:  safeStr(target, func(t *DeliveryTarget) string { return t.AccountID }),
		})

		if runErr != nil {
			outcome = RunOutcome{
				Status:     "error",
				Error:      runErr.Error(),
				StartedAt:  startedAt,
				EndedAt:    time.Now().UnixMilli(),
				DurationMs: time.Now().UnixMilli() - startedAt,
			}
		} else {
			outcome = RunOutcome{
				Status:     "ok",
				Output:     output,
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

	// Apply result to job state (mirrors job-result.ts applyJobResult).
	s.applyJobResult(job, outcome)

	// Log run.
	s.runLog.Append(RunLogEntry{
		Ts:          time.Now().UnixMilli(),
		JobID:       job.ID,
		Action:      "finished",
		Status:      outcome.Status,
		Error:       outcome.Error,
		Summary:     PickSummaryFromOutput(outcome.Output),
		DurationMs:  outcome.DurationMs,
		NextRunAtMs: ComputeNextRunAtMs(job.Schedule, time.Now().UnixMilli()),
	})

	s.emit(CronEvent{Type: "job_finished", JobID: job.ID, Status: outcome.Status})
	return outcome
}

// applyJobResult updates the job state after execution (mirrors job-result.ts).
func (s *Service) applyJobResult(job StoreJob, outcome RunOutcome) {
	state := job.State
	state.LastRunAtMs = outcome.StartedAt
	state.LastDurationMs = outcome.DurationMs
	state.LastRunStatus = outcome.Status
	state.RunningAtMs = 0

	if outcome.Status == "ok" {
		state.ConsecutiveErrors = 0
		state.LastError = ""
	} else {
		state.ConsecutiveErrors++
		state.LastError = outcome.Error
		// Auto-disable after 10 consecutive schedule errors.
		if state.ConsecutiveErrors >= 10 {
			state.ScheduleErrorCount++
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
