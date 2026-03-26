// isolated_agent.go — Full isolated agent execution for cron jobs.
// Mirrors src/cron/isolated-agent/run.ts (930 LOC),
// delivery-dispatch.ts (637 LOC), delivery-target.ts (180 LOC),
// session.ts (90 LOC), session-key.ts (13 LOC),
// subagent-followup.ts (200 LOC), helpers.ts (86 LOC),
// skills-snapshot.ts (37 LOC).
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// IsolatedAgentConfig configures an isolated cron agent turn.
type IsolatedAgentConfig struct {
	Job            StoreJob
	AgentID        string
	SessionKey     string
	RunSessionID   string
	WorkspaceDir   string
	DefaultChannel string
	DefaultTo      string
	TimeoutMs      int64
	// Model overrides from job payload.
	Model      string
	Thinking   string
	Fallbacks  []string
	// Delivery configuration.
	DeliveryTarget     *DeliveryTarget
	DeliveryBestEffort bool
	SkipHeartbeatDelivery bool
}

// IsolatedAgentResult holds the full outcome of an isolated agent run.
type IsolatedAgentResult struct {
	Outcome        RunOutcome
	DeliveryResult *DeliveryResult
	Summary        string
	OutputText     string
	Payloads       []autoreply.ReplyPayload
	WasHeartbeat   bool
	SessionKey     string
}

// RunIsolatedAgentTurn executes a full cron job agent turn with delivery.
// This mirrors the main runCronIsolatedAgentTurn() from the TS codebase.
func RunIsolatedAgentTurn(
	ctx context.Context,
	cfg IsolatedAgentConfig,
	agent AgentRunner,
	channels *channel.Registry,
	logger *slog.Logger,
) IsolatedAgentResult {
	startedAt := time.Now().UnixMilli()
	result := IsolatedAgentResult{
		SessionKey: cfg.SessionKey,
	}

	// 1. Apply timeout.
	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5 * 60 * 1000
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// 2. Resolve command from job payload.
	command := cfg.Job.Payload.Message
	if cfg.Job.Payload.Kind == "systemEvent" {
		command = cfg.Job.Payload.Text
	}
	if command == "" {
		result.Outcome = RunOutcome{
			Status:     "error",
			Error:      "empty command in cron job payload",
			StartedAt:  startedAt,
			EndedAt:    time.Now().UnixMilli(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
		return result
	}

	// 3. Run the agent turn.
	output, runErr := agent.RunAgentTurn(runCtx, AgentTurnParams{
		SessionKey: cfg.SessionKey,
		AgentID:    cfg.AgentID,
		Command:    command,
		Channel:    safeTargetField(cfg.DeliveryTarget, "channel"),
		To:         safeTargetField(cfg.DeliveryTarget, "to"),
		AccountID:  safeTargetField(cfg.DeliveryTarget, "accountId"),
	})

	endedAt := time.Now().UnixMilli()

	if runErr != nil {
		status := "error"
		if runCtx.Err() == context.DeadlineExceeded {
			status = "timeout"
		}
		result.Outcome = RunOutcome{
			Status:     status,
			Error:      runErr.Error(),
			StartedAt:  startedAt,
			EndedAt:    endedAt,
			DurationMs: endedAt - startedAt,
		}
		return result
	}

	result.OutputText = output
	result.Summary = PickSummaryFromOutput(output)

	// 4. Check if output is heartbeat-only.
	stripped := autoreply.StripHeartbeatToken(output, autoreply.StripModeHeartbeat, autoreply.DefaultHeartbeatAckChars)
	if stripped.ShouldSkip {
		result.WasHeartbeat = true
		if cfg.SkipHeartbeatDelivery {
			result.Outcome = RunOutcome{
				Status:     "ok",
				Output:     output,
				StartedAt:  startedAt,
				EndedAt:    endedAt,
				DurationMs: endedAt - startedAt,
			}
			return result
		}
	}

	// 5. Check for subagent followup.
	if isLikelyInterimMessage(output) {
		logger.Debug("likely interim message, waiting for subagent", "session", cfg.SessionKey)
		// In the full TS implementation, this waits for descendant subagents.
		// For the Go gateway, we proceed with what we have.
	}

	// 6. Build delivery payloads.
	deliveryText := output
	if stripped.DidStrip && stripped.Text != "" {
		deliveryText = stripped.Text
	}
	if deliveryText != "" {
		result.Payloads = append(result.Payloads, autoreply.ReplyPayload{Text: deliveryText})
	}

	// 7. Deliver to target.
	if cfg.DeliveryTarget != nil && len(result.Payloads) > 0 && !result.WasHeartbeat {
		dr := DeliverCronOutput(runCtx, channels, *cfg.DeliveryTarget, result.Payloads, DeliverOutputOptions{
			ChunkLimit: autoreply.DefaultChunkLimit,
			ChunkMode:  "length",
			BestEffort: cfg.DeliveryBestEffort,
			Logger:     logger,
		})
		result.DeliveryResult = &dr
	}

	result.Outcome = RunOutcome{
		Status:     "ok",
		Output:     output,
		Delivery:   result.DeliveryResult,
		StartedAt:  startedAt,
		EndedAt:    time.Now().UnixMilli(),
		DurationMs: time.Now().UnixMilli() - startedAt,
	}
	return result
}

// isLikelyInterimMessage checks if agent output looks like an interim ack
// (suggesting subagents are still running).
func isLikelyInterimMessage(text string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	// Very short "working on it" type responses.
	interimPatterns := []string{
		"working on", "let me", "i'll", "one moment", "processing",
		"looking into", "checking", "running", "executing",
	}
	if len(trimmed) < 100 {
		for _, pattern := range interimPatterns {
			if strings.Contains(lower, pattern) {
				return true
			}
		}
	}
	return false
}

// --- Session key resolution ---

// ResolveCronAgentSessionKey builds the session key for a cron agent turn.
func ResolveCronAgentSessionKey(agentID, jobID string) string {
	if agentID == "" {
		agentID = "main"
	}
	return fmt.Sprintf("agent:%s:cron:%s", agentID, jobID)
}

// ResolveCronRunSessionKey builds a unique session key for a single cron run.
func ResolveCronRunSessionKey(agentID, jobID string, runAtMs int64) string {
	base := ResolveCronAgentSessionKey(agentID, jobID)
	return fmt.Sprintf("%s:run:%d", base, runAtMs)
}

// --- Session freshness ---

// CronSessionFreshness evaluates whether a cron session should be reused or reset.
type CronSessionFreshness struct {
	ShouldReset bool
	Reason      string
}

// EvaluateCronSessionFreshness checks if the cron session should be reused.
func EvaluateCronSessionFreshness(lastRunAtMs, nowMs, maxAgeMs int64) CronSessionFreshness {
	if maxAgeMs <= 0 {
		return CronSessionFreshness{ShouldReset: false}
	}
	if lastRunAtMs <= 0 {
		return CronSessionFreshness{ShouldReset: true, Reason: "no previous run"}
	}
	age := nowMs - lastRunAtMs
	if age > maxAgeMs {
		return CronSessionFreshness{
			ShouldReset: true,
			Reason:      fmt.Sprintf("session age %dms exceeds max %dms", age, maxAgeMs),
		}
	}
	return CronSessionFreshness{ShouldReset: false}
}

// --- Skills snapshot ---

// CronSkillsSnapshot captures available skills for a cron run.
type CronSkillsSnapshot struct {
	Skills []string
}

// ResolveCronSkillsSnapshot builds the skills snapshot for filtering.
func ResolveCronSkillsSnapshot(allSkills []string, filter []string) CronSkillsSnapshot {
	if len(filter) == 0 {
		return CronSkillsSnapshot{Skills: allSkills}
	}
	filterSet := make(map[string]bool, len(filter))
	for _, f := range filter {
		filterSet[strings.ToLower(f)] = true
	}
	var filtered []string
	for _, s := range allSkills {
		if filterSet[strings.ToLower(s)] {
			filtered = append(filtered, s)
		}
	}
	return CronSkillsSnapshot{Skills: filtered}
}

func safeTargetField(target *DeliveryTarget, field string) string {
	if target == nil {
		return ""
	}
	switch field {
	case "channel":
		return target.Channel
	case "to":
		return target.To
	case "accountId":
		return target.AccountID
	default:
		return ""
	}
}
