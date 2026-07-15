package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"

// handleGoalCommand processes "/goal …" via toolwire so the chat parent does
// not import domain/goals.
func (h *Handler) handleGoalCommand(sessionKey, args string, respond func(text string)) {
	toolwire.HandleGoalCommand(sessionKey, args, respond)
}
