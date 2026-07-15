package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"

// Type aliases — canonical definitions are in toolport/.

// TurnContext is a thread-safe store for sharing tool results within a single agent turn.
type TurnContext = toolport.TurnContext

// toolTimingStats is a snapshot of aggregated completion times for a tool within a turn.
type toolTimingStats = toolport.ToolTimingStats

// turnResult holds the outcome of a single tool execution within a turn.
// Previously named turnResult_ (unexported); now exported via toolport.
type turnResult = toolport.TurnResult

// newTurnContext creates an empty turn context for a new agent turn.
func newTurnContext() *TurnContext { return toolport.NewTurnContext() }

// detectCycle checks whether the given $ref map forms a cycle.
// Returns a descriptive error naming the cycle if one is found.
func detectCycle(refs map[string]string) error { return toolport.DetectCycle(refs) }
