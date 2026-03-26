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
// It sends a message via SessionsSend, subscribes to transcript appends to capture
// the assistant output, and waits for the run to complete via JobTracker.
type autonomousAgentAdapter struct {
	chatHandler *chat.Handler
	jobTracker  *agent.JobTracker
	transcript  *transcript.Writer
}

// RunAgentTurn implements autonomous.AgentRunner.
func (a *autonomousAgentAdapter) RunAgentTurn(ctx context.Context, sessionKey, message string) (string, error) {
	runID := fmt.Sprintf("autonomous_%d", time.Now().UnixNano())

	// Subscribe to transcript appends for this session BEFORE starting the run,
	// so we don't miss the assistant message.
	var lastAssistantMsg string
	outputCh := make(chan string, 1)
	if a.transcript != nil {
		unsubscribe := a.transcript.OnAppend(func(key string, msg json.RawMessage) {
			if key != sessionKey {
				return
			}
			var parsed struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if json.Unmarshal(msg, &parsed) == nil && parsed.Role == "assistant" && parsed.Content != "" {
				// Non-blocking send — only capture the latest.
				select {
				case outputCh <- parsed.Content:
				default:
					// Drain and replace with newer message.
					select {
					case <-outputCh:
					default:
					}
					outputCh <- parsed.Content
				}
			}
		})
		defer unsubscribe()
	}

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

	// Wait for the run to complete via JobTracker.
	if a.jobTracker != nil {
		timeoutMs := int64(10 * 60 * 1000) // 10 minutes
		snap := a.jobTracker.WaitForJob(ctx, runID, timeoutMs, false)
		if snap != nil && snap.Status == agent.RunStatusError {
			return "", fmt.Errorf("agent run failed: %s", snap.Error)
		}
	}

	// Collect the output captured by the transcript listener.
	select {
	case lastAssistantMsg = <-outputCh:
	default:
		// No output captured — may have been a silent reply.
	}

	return lastAssistantMsg, nil
}
