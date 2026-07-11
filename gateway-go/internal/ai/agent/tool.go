// tool.go — ToolExecutor interface for the agent executor.
package agent

import (
	"context"
	"encoding/json"
	"errors"
)

// ToolExecutor executes a named tool with JSON input and returns the result.
// An error return causes the tool result to be marked as is_error=true in the
// LLM conversation, allowing the model to handle the failure gracefully.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// ErrUnknownTool is the sentinel an executor implementation wraps when the
// model called a tool name that does not exist (hallucinated or typoed).
// The agent loop tags such calls in turn.tool (agentlog) so the cross-session
// aggregate can measure how often the model invents tool names — a direct
// signal for deferred-description/selection-quality work. Wrap it with
// fmt.Errorf("%w: ...", agent.ErrUnknownTool, ...) so errors.Is matches.
var ErrUnknownTool = errors.New("unknown tool")
