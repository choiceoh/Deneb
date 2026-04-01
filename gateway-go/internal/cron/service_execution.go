package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
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
		if s.cfg.Sessions != nil {
			s.cfg.Sessions.Create(sessionKey, session.KindCron)
		}

		output, runErr := s.agent.RunAgentTurn(runCtx, AgentTurnParams{
			SessionKey: sessionKey,
			AgentID:    job.AgentID,
			Command:    command,
			Channel:    safeStr(target, func(t *DeliveryTarget) string { return t.Channel }),
			To:         safeStr(target, func(t *DeliveryTarget) string { return t.To }),
			AccountID:  safeStr(target, func(t *DeliveryTarget) string { return t.AccountID }),
		})

		if runErr != nil {
			status := "error"
			if runCtx.Err() == context.DeadlineExceeded {
				status = "timeout"
			}
			outcome = RunOutcome{
				Status:     status,
				Error:      runErr.Error(),
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

	text := fmt.Sprintf("⚠️ Cron job %q failed: %s", job.Name, outcome.Error)
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

// createShadowSession creates a KindShadow session that inherits recent
// conversation context from the main session. Returns the shadow session key,
// or empty string if shadow sessions are not configured.
const shadowContextLimit = 20 // number of recent messages to clone

func (s *Service) createShadowSession(cronSessionKey string) string {
	if s.cfg.Sessions == nil || s.cfg.TranscriptCloner == nil || s.cfg.MainSessionKey == "" {
		return ""
	}
	shadowKey := "shadow:" + cronSessionKey
	s.cfg.Sessions.Create(shadowKey, session.KindShadow)
	if err := s.cfg.TranscriptCloner.CloneRecent(s.cfg.MainSessionKey, shadowKey, shadowContextLimit); err != nil {
		s.logger.Warn("shadow session clone failed", "key", shadowKey, "error", err)
	}
	return shadowKey
}
