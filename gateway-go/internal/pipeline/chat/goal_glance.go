package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// GoalGlanceFunc returns a compact, pre-formatted snapshot of the session's
// active standing goal for the dynamic system-prompt block, or "" when there is
// no active goal. Construction lives in toolwire so the chat parent does not
// import domain/goals.
type GoalGlanceFunc func(ctx context.Context, sessionKey string) string

// NewGoalGlanceFunc builds the ambient goal glance from the process goal store.
func NewGoalGlanceFunc() GoalGlanceFunc {
	return toolwire.NewGoalGlanceFunc()
}
