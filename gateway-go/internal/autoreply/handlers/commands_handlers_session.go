// commands_handlers_session.go — Session command handlers.
package handlers

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"

func handleResetCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{
		Reply:      "🔄 Session reset.",
		SessionMod: &types.SessionModification{Reset: true},
		SkipAgent:  true,
	}, nil
}

func handleStopCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "⏹ Stopped.", SkipAgent: true}, nil
}

func handleCancelCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "❌ Cancelled.", SkipAgent: true}, nil
}

func handleKillCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "💀 Killed.", SkipAgent: true}, nil
}

func handleCompactCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "📦 Context compacted.", SkipAgent: true}, nil
}
