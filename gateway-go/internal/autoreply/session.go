package autoreply

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/commands"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// DetectResetTrigger checks if the message body is a session reset command.
// This function stays in autoreply because it depends on *CommandRegistry.
func DetectResetTrigger(body string, registry *commands.CommandRegistry) types.SessionResetTrigger {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return types.ResetNone
	}
	normalized := trimmed
	if registry != nil {
		normalized = registry.NormalizeCommandBody(trimmed, "")
	}
	lowered := strings.ToLower(normalized)
	if lowered == "/new" || lowered == "/reset" {
		return types.ResetCommand
	}
	return types.ResetNone
}
