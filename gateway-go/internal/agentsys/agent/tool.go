// tool.go — ToolExecutor interface for the agent executor.
package agent

import (
	"context"
	"encoding/json"
)

// ToolExecutor executes a named tool with JSON input and returns the result.
// An error return causes the tool result to be marked as is_error=true in the
// LLM conversation, allowing the model to handle the failure gracefully.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}
