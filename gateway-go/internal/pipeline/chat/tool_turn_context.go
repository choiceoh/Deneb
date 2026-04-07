package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"

// Type aliases — canonical definitions are in toolctx/.

// TurnContext is a thread-safe store for sharing tool results within a single agent turn.
type TurnContext = toolctx.TurnContext

// ToolTimingStats is a snapshot of aggregated completion times for a tool within a turn.
type ToolTimingStats = toolctx.ToolTimingStats

// TurnResult holds the outcome of a single tool execution within a turn.
// Previously named turnResult_ (unexported); now exported via toolctx.
type TurnResult = toolctx.TurnResult

// NewTurnContext creates an empty turn context for a new agent turn.
func NewTurnContext() *TurnContext { return toolctx.NewTurnContext() }

// DetectCycle checks whether the given $ref map forms a cycle.
// Returns a descriptive error naming the cycle if one is found.
func DetectCycle(refs map[string]string) error { return toolctx.DetectCycle(refs) }
