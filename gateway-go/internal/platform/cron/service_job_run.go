package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// cronJobRun holds the immutable execution snapshot and the bounded context
// shared by the agent, subagent polling, and delivery stages of one cron run.
type cronJobRun struct {
	service     *Service
	callerCtx   context.Context
	runCtx      context.Context
	cancel      context.CancelFunc
	job         StoreJob
	trigger     triggerSource
	startedAt   int64
	timeoutMs   int64
	deliveryCfg *JobDeliveryConfig
	target      *DeliveryTarget
	targetErr   error
	command     string
	sessionKey  string
}

func (s *Service) beginCronJobRun(job StoreJob) (RunOutcome, bool) {
	if _, loaded := s.runningJobs.LoadOrStore(job.ID, true); !loaded {
		return RunOutcome{}, true
	}
	s.logger.Info("cron job already running, skipping", "id", job.ID)
	s.runLog.Append(RunLogEntry{ //nolint:errcheck // best-effort
		Ts:     time.Now().UnixMilli(),
		JobID:  job.ID,
		Action: "skipped",
		Status: "skipped",
		Error:  "concurrent execution prevented",
	})
	s.markPendingRerun(job.ID, "overlap trigger skipped")
	return RunOutcome{
		Status:    "skipped",
		Error:     "job already running",
		StartedAt: time.Now().UnixMilli(),
		EndedAt:   time.Now().UnixMilli(),
	}, false
}

func (s *Service) newCronJobRun(ctx context.Context, job StoreJob, trigger triggerSource) *cronJobRun {
	startedAt := time.Now().UnixMilli()
	s.emit(CronEvent{Type: "job_started", JobID: job.ID})
	timeoutMs := ResolveCronJobTimeoutMs(job)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	deliveryCfg := job.Delivery
	target, targetErr := ResolveDeliveryTarget(deliveryCfg, s.cfg.DefaultChannel, s.cfg.DefaultTo)
	return &cronJobRun{
		service:     s,
		callerCtx:   ctx,
		runCtx:      runCtx,
		cancel:      cancel,
		job:         job,
		trigger:     trigger,
		startedAt:   startedAt,
		timeoutMs:   timeoutMs,
		deliveryCfg: deliveryCfg,
		target:      target,
		targetErr:   targetErr,
		command:     resolveJobCommand(job),
		sessionKey:  fmt.Sprintf("cron:%s:%d", job.ID, startedAt),
	}
}

func (r *cronJobRun) close() {
	r.cancel()
}

func (r *cronJobRun) execute() RunOutcome {
	switch {
	case r.targetErr != nil && !isBestEffort(r.deliveryCfg):
		return r.outcome("error", fmt.Sprintf("delivery target error: %s", r.targetErr), "", nil, 0)
	case r.service.agent == nil:
		return r.outcome("error", "no agent runner configured", "", nil, 0)
	default:
		return r.executeAgent()
	}
}

func (r *cronJobRun) executeAgent() RunOutcome {
	sessionKind := r.prepareSession()
	output, retriesUsed, runErr := r.runAgentWithRetries(sessionKind)
	if runErr != nil {
		return r.failedAgentOutcome(runErr, retriesUsed)
	}
	output = pollSubagentOutputs(r.runCtx, r.service.cfg.SubagentPoller, r.sessionKey, output)
	delivery := r.deliverOutput(output)
	status, errMsg := r.deliveryStatus(delivery)
	return r.outcome(status, errMsg, output, delivery, retriesUsed)
}

func (r *cronJobRun) prepareSession() session.Kind {
	sessionKind := session.KindCron
	if r.job.SessionTarget == SessionTargetSubagent && r.service.cfg.TranscriptCloner != nil && r.service.cfg.MainSessionKey != "" {
		if err := r.service.cfg.TranscriptCloner.CloneRecent(r.service.cfg.MainSessionKey, r.sessionKey, 20); err != nil {
			r.service.logger.Warn("cron session transcript clone failed", "jobId", r.job.ID, "error", err)
		}
	}
	if r.service.cfg.Sessions != nil {
		r.service.cfg.Sessions.Create(r.sessionKey, sessionKind)
	}
	return sessionKind
}

func (r *cronJobRun) runAgentWithRetries(sessionKind session.Kind) (string, int, error) {
	maxRetries := boundedCronRetries(r.job.Payload.RetryCount)
	retryBackoff := cronRetryBackoffMs(r.job.Payload.RetryBackoffMs)
	var output string
	var runErr error
	retriesUsed := 0
	for attempt := 0; attempt <= maxRetries; attempt++ {
		output, runErr = r.service.agent.RunAgentTurn(r.runCtx, r.agentTurnParams(sessionKind))
		if runErr == nil || r.runCtx.Err() != nil || attempt >= maxRetries {
			break
		}
		retriesUsed++
		backoff := cronRetryDelay(retryBackoff, attempt)
		r.service.logger.Info("cron job retrying", "id", r.job.ID, "attempt", attempt+2, "of", maxRetries+1, "backoff", backoff)
		select {
		case <-r.runCtx.Done():
			runErr = r.runCtx.Err()
		case <-time.After(backoff):
		}
		if r.runCtx.Err() != nil {
			break
		}
	}
	return output, retriesUsed, runErr
}

func boundedCronRetries(retries int) int {
	if retries < 0 {
		return 0
	}
	if retries > 3 {
		return 3
	}
	return retries
}

func cronRetryBackoffMs(backoffMs int64) int64 {
	if backoffMs <= 0 {
		return 5000
	}
	return backoffMs
}

func cronRetryDelay(backoffMs int64, attempt int) time.Duration {
	backoff := time.Duration(backoffMs<<uint(attempt)) * time.Millisecond
	if backoff > 60*time.Second {
		return 60 * time.Second
	}
	return backoff
}

func (r *cronJobRun) agentTurnParams(sessionKind session.Kind) AgentTurnParams {
	return AgentTurnParams{
		SessionKey:  r.sessionKey,
		SessionKind: sessionKind,
		AgentID:     r.job.AgentID,
		Command:     r.command,
		Channel:     safeStr(r.target, func(target *DeliveryTarget) string { return target.Channel }),
		To:          safeStr(r.target, func(target *DeliveryTarget) string { return target.To }),
		AccountID:   safeStr(r.target, func(target *DeliveryTarget) string { return target.AccountID }),
		ThreadID:    safeStr(r.target, func(target *DeliveryTarget) string { return target.ThreadID }),
		Thinking:    r.job.Payload.Thinking,
	}
}

func (r *cronJobRun) failedAgentOutcome(runErr error, retriesUsed int) RunOutcome {
	status := "error"
	errMsg := runErr.Error()
	if errors.Is(runErr, ErrTurnAborted) {
		status = "aborted"
	} else if r.runCtx.Err() == context.DeadlineExceeded {
		status = "timeout"
		elapsed := time.Duration(time.Now().UnixMilli()-r.startedAt) * time.Millisecond
		timeout := time.Duration(r.timeoutMs) * time.Millisecond
		errMsg = fmt.Sprintf("timeout after %s (limit: %s)", elapsed.Round(time.Second), timeout.Round(time.Second))
	}
	return r.outcome(status, errMsg, "", nil, retriesUsed)
}

func (r *cronJobRun) deliverOutput(output string) *DeliveryResult {
	if output == "" || r.target == nil {
		return nil
	}
	stripped := tokens.StripHeartbeatToken(output, tokens.StripModeHeartbeat, tokens.DefaultHeartbeatAckChars)
	if stripped.ShouldSkip {
		return nil
	}
	deliveryText := output
	if stripped.DidStrip && stripped.Text != "" {
		deliveryText = stripped.Text
	}
	if r.service.cfg.MainSessionHandoff == nil {
		return nil
	}
	handled, err := r.service.cfg.MainSessionHandoff(
		r.runCtx, r.target.Channel, r.target.To, r.job.ID, deliveryText,
	)
	if err != nil {
		r.service.logger.Warn("cron main-session handoff failed",
			"jobId", r.job.ID,
			"channel", r.target.Channel,
			"to", r.target.To,
			"error", err)
		return &DeliveryResult{
			Delivered: false,
			Channel:   r.target.Channel,
			To:        r.target.To,
			Error:     err.Error(),
		}
	}
	if !handled {
		return nil
	}
	return &DeliveryResult{Delivered: true, Channel: r.target.Channel, To: r.target.To}
}

func (r *cronJobRun) deliveryStatus(delivery *DeliveryResult) (string, string) {
	if delivery != nil && !delivery.Delivered && !isBestEffort(r.deliveryCfg) {
		return "error", "delivery failed: " + delivery.Error
	}
	return "ok", ""
}

func (r *cronJobRun) outcome(status, errMsg, output string, delivery *DeliveryResult, retries int) RunOutcome {
	return RunOutcome{
		Status:     status,
		Error:      errMsg,
		Output:     output,
		Delivery:   delivery,
		Retries:    retries,
		StartedAt:  r.startedAt,
		EndedAt:    time.Now().UnixMilli(),
		DurationMs: time.Now().UnixMilli() - r.startedAt,
	}
}

func (r *cronJobRun) finalize(outcome RunOutcome) {
	r.service.applyJobResult(r.job, outcome, r.sessionKey, r.trigger)
	r.sendFailureAlert(outcome)
	r.service.runLog.Append(r.runLogEntry(outcome)) //nolint:errcheck // best-effort
	r.service.emit(CronEvent{Type: "job_finished", JobID: r.job.ID, Status: outcome.Status})
}

func (r *cronJobRun) sendFailureAlert(outcome RunOutcome) {
	if r.service.cfg.MainSessionHandoff == nil {
		return
	}
	if ShouldSendFailureAlert(r.job.State, r.job.FailureAlert, outcome.Status, time.Now().UnixMilli()) {
		r.service.sendFailureAlert(r.callerCtx, r.job, outcome)
	}
}

func (r *cronJobRun) runLogEntry(outcome RunOutcome) RunLogEntry {
	entry := RunLogEntry{
		Ts:          time.Now().UnixMilli(),
		JobID:       r.job.ID,
		Action:      "finished",
		Status:      outcome.Status,
		Error:       outcome.Error,
		Summary:     PickSummaryFromOutput(outcome.Output),
		DurationMs:  outcome.DurationMs,
		NextRunAtMs: ComputeNextRunAtMs(r.job.Schedule, time.Now().UnixMilli()),
		Retries:     outcome.Retries,
	}
	if outcome.Delivery == nil {
		return entry
	}
	entry.Delivered = outcome.Delivery.Delivered
	if outcome.Delivery.Delivered {
		entry.DeliveryStatus = "delivered"
	} else {
		entry.DeliveryStatus = "not-delivered"
		entry.DeliveryError = outcome.Delivery.Error
	}
	return entry
}
