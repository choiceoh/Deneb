package toolwire

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/wire"
)

// NewGoalGlanceFunc builds the ambient standing-goal glance for the dynamic system prompt.
func NewGoalGlanceFunc() func(ctx context.Context, sessionKey string) string {
	return wire.NewGoalGlanceFunc()
}

// HandleGoalCommand processes the /goal slash command against the process goal store.
func HandleGoalCommand(sessionKey, args string, respond func(text string)) {
	wire.HandleGoalCommand(sessionKey, args, respond)
}

// ToolMaxOutputs returns per-tool output character budgets from tool_schemas.json.
func ToolMaxOutputs() map[string]int {
	return wire.ToolMaxOutputs()
}
