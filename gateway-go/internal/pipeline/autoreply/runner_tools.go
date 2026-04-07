package autoreply

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

func (r *DefaultAgentRunner) executeTool(ctx context.Context, call ToolCall, cfg AgentTurnConfig) (result string, isErr bool, err error) {
	if r.tools == nil {
		return "", true, fmt.Errorf("no tool executor configured")
	}

	// Check elevated permissions for bash/exec tools.
	if (call.Name == "bash" || call.Name == "execute" || call.Name == "computer") &&
		cfg.ElevatedLevel == types.ElevatedOff {
		return "Tool execution requires elevated permissions. Use /elevated on to enable.", true, nil
	}

	// Approval requirement: in approval mode, we'd normally pause and wait for
	// user approval. For now, auto-approve in the Go gateway (matches DGX
	// Spark single-user model), so no action is needed here.

	return r.tools.Execute(ctx, call)
}

// ReminderGuard prevents infinite reminder loops during agent execution.
type ReminderGuard struct {
	mu       sync.Mutex
	count    int
	maxCount int
}

func NewReminderGuard(maxCount int) *ReminderGuard {
	if maxCount <= 0 {
		maxCount = 3
	}
	return &ReminderGuard{maxCount: maxCount}
}

func (g *ReminderGuard) TryRemind() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.count >= g.maxCount {
		return false
	}
	g.count++
	return true
}

func (g *ReminderGuard) Reset() {
	g.mu.Lock()
	g.count = 0
	g.mu.Unlock()
}

func formatToolInput(input map[string]any) string {
	if input == nil {
		return ""
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func truncateToolOutput(output string, maxLen int) string {
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "…[truncated]"
}
