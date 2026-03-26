package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// RunOutcome represents the result of a cron job execution.
type RunOutcome struct {
	Status     string          `json:"status"` // "ok", "error", "skipped", "timeout"
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	Delivery   *DeliveryResult `json:"delivery,omitempty"`
	StartedAt  int64           `json:"startedAt"`
	EndedAt    int64           `json:"endedAt"`
	DurationMs int64           `json:"durationMs"`
}

// Job represents a cron job definition with its schedule and delivery config.
type Job struct {
	ID        string             `json:"id"`
	AgentID   string             `json:"agentId,omitempty"`
	Command   string             `json:"command"` // prompt or command text
	Schedule  Schedule           `json:"schedule"`
	Delivery  *JobDeliveryConfig `json:"delivery,omitempty"`
	TimeoutMs int64              `json:"timeoutMs,omitempty"`
	Enabled   bool               `json:"enabled"`
}

// AgentRunner abstracts the agent execution so the cron package does not
// depend on chat.Handler or protocol (which pull in CGo/FFI).
type AgentRunner interface {
	// RunAgentTurn executes an agent turn for a cron job and returns the text output.
	// It blocks until the agent completes or the context is canceled.
	RunAgentTurn(ctx context.Context, params AgentTurnParams) (output string, err error)
}

// AgentTurnParams holds parameters for a single cron agent turn.
type AgentTurnParams struct {
	SessionKey string
	AgentID    string
	Command    string
	Channel    string
	To         string
	AccountID  string
	ThreadID   string
}

// RunnerDeps holds the dependencies for the cron job runner.
type RunnerDeps struct {
	Sessions       *session.Manager
	Channels       *channel.Registry
	Agent          AgentRunner
	Logger         *slog.Logger
	DefaultChannel string // default delivery channel (e.g., "telegram")
	DefaultTo      string // default delivery recipient (e.g., chat ID)
}

// RunJob executes a single cron job: runs the agent turn and delivers output.
// This is the Go equivalent of runCronIsolatedAgentTurn() from the TS codebase.
func RunJob(ctx context.Context, job Job, deps RunnerDeps) RunOutcome {
	startedAt := time.Now().UnixMilli()

	// Apply timeout.
	timeoutMs := job.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5 * 60 * 1000 // 5 minutes default
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Resolve delivery target.
	target, err := ResolveDeliveryTarget(job.Delivery, deps.DefaultChannel, deps.DefaultTo)
	if err != nil {
		endedAt := time.Now().UnixMilli()
		return RunOutcome{
			Status:     "error",
			Error:      fmt.Sprintf("delivery target resolution failed: %s", err),
			StartedAt:  startedAt,
			EndedAt:    endedAt,
			DurationMs: endedAt - startedAt,
		}
	}

	// Build session key for this cron run.
	sessionKey := fmt.Sprintf("cron:%s:%d", job.ID, startedAt)
	agentID := job.AgentID
	if agentID == "" {
		agentID = "main"
	}

	// Create or get session.
	sess := deps.Sessions.Get(sessionKey)
	if sess == nil {
		deps.Sessions.Create(sessionKey, session.KindDirect)
	}

	// Run the agent turn.
	output, runErr := deps.Agent.RunAgentTurn(runCtx, AgentTurnParams{
		SessionKey: sessionKey,
		AgentID:    agentID,
		Command:    job.Command,
		Channel:    target.Channel,
		To:         target.To,
		AccountID:  target.AccountID,
		ThreadID:   target.ThreadID,
	})

	if runErr != nil {
		endedAt := time.Now().UnixMilli()
		status := "error"
		if runCtx.Err() == context.DeadlineExceeded {
			status = "timeout"
		}
		return RunOutcome{
			Status:     status,
			Error:      runErr.Error(),
			StartedAt:  startedAt,
			EndedAt:    endedAt,
			DurationMs: endedAt - startedAt,
		}
	}

	// Deliver output to target channel.
	var deliveryResult *DeliveryResult
	if output != "" && target != nil {
		// Skip delivery if the output is just a heartbeat ack.
		stripped := autoreply.StripHeartbeatToken(output, autoreply.StripModeHeartbeat, 0)
		if !stripped.ShouldSkip {
			payloads := []autoreply.ReplyPayload{{Text: stripped.Text}}
			bestEffort := false
			if job.Delivery != nil {
				bestEffort = job.Delivery.BestEffort
			}
			dr := DeliverCronOutput(runCtx, deps.Channels, *target, payloads, DeliverOutputOptions{
				ChunkLimit: autoreply.DefaultChunkLimit,
				ChunkMode:  "length",
				BestEffort: bestEffort,
				Logger:     deps.Logger,
			})
			deliveryResult = &dr
		}
	}

	finalEndedAt := time.Now().UnixMilli()
	return RunOutcome{
		Status:     "ok",
		Output:     output,
		Delivery:   deliveryResult,
		StartedAt:  startedAt,
		EndedAt:    finalEndedAt,
		DurationMs: finalEndedAt - startedAt,
	}
}
