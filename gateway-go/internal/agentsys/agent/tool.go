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

// ConcurrencyChecker is an optional interface that ToolExecutor implementations
// can satisfy to declare which tools are safe for parallel execution. When the
// executor receives a ToolExecutor that implements this interface, it uses
// IsConcurrencySafe instead of the built-in fallback set.
type ConcurrencyChecker interface {
	IsConcurrencySafe(name string) bool
}

// InputAwareConcurrencyChecker extends ConcurrencyChecker to consider the
// tool's input when determining concurrency safety. For example, an "exec"
// tool running "go test" is read-only and safe for concurrent execution,
// while "rm -rf" is not. When the executor finds this interface, it uses
// IsConcurrencySafeWithInput for batching decisions.
type InputAwareConcurrencyChecker interface {
	ConcurrencyChecker
	IsConcurrencySafeWithInput(name string, input json.RawMessage) bool
}
