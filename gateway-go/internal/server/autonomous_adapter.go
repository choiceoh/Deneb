package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// autonomousAgentAdapter bridges chat.Handler to the autonomous.AgentRunner interface.
// It sends a message via SessionsSend, waits for the run to complete via JobTracker,
// and reads the output from the transcript.
type autonomousAgentAdapter struct {
	chatHandler *chat.Handler
	jobTracker  *agent.JobTracker
	transcript  *transcript.Writer
}

// RunAgentTurn implements autonomous.AgentRunner.
func (a *autonomousAgentAdapter) RunAgentTurn(ctx context.Context, sessionKey, message string) (string, error) {
	runID := fmt.Sprintf("autonomous_%d", time.Now().UnixNano())

	// Build the sessions.send request.
	req := &protocol.RequestFrame{
		ID:     runID,
		Method: "sessions.send",
	}
	params := map[string]any{
		"key":            sessionKey,
		"message":        message,
		"idempotencyKey": runID,
	}
	req.Params, _ = json.Marshal(params)

	// Fire the async agent run.
	resp := a.chatHandler.SessionsSend(ctx, req)
	if resp != nil && resp.Error != nil {
		return "", fmt.Errorf("sessions.send failed: %s", resp.Error.Message)
	}

	// Wait for the run to complete.
	if a.jobTracker != nil {
		timeoutMs := int64(10 * 60 * 1000) // 10 minutes
		snap := a.jobTracker.WaitForJob(ctx, runID, timeoutMs, false)
		if snap != nil && snap.Status == agent.RunStatusError {
			return "", fmt.Errorf("agent run failed: %s", snap.Error)
		}
	} else {
		// No job tracker — poll-wait with simple sleep.
		// This is a fallback; in production jobTracker should always be set.
		time.Sleep(5 * time.Second)
	}

	// Read the last assistant message from the transcript.
	if a.transcript != nil {
		msgs, err := a.transcript.ReadMessages(sessionKey)
		if err == nil && len(msgs) > 0 {
			// Walk backward to find the last assistant message.
			for i := len(msgs) - 1; i >= 0; i-- {
				var msg struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}
				if json.Unmarshal(msgs[i], &msg) == nil && msg.Role == "assistant" {
					return msg.Content, nil
				}
			}
		}
	}

	return "", nil
}
