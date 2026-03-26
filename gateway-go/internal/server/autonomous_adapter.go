package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	// so we don't miss any assistant messages. Accumulate all assistant content
	// so that goal_update blocks in intermediate messages are not lost.
	var mu sync.Mutex
	var accumulated strings.Builder
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
				mu.Lock()
				if accumulated.Len() > 0 {
					accumulated.WriteString("\n")
				}
				accumulated.WriteString(parsed.Content)
				mu.Unlock()
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

	// Return the accumulated assistant output.
	mu.Lock()
	output := accumulated.String()
	mu.Unlock()
	return output, nil
}
