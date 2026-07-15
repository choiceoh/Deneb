package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// goalGlanceFunc returns a compact, pre-formatted snapshot of the session's
// active standing goal for the dynamic system-prompt block, or "" when there is
// no active goal. Construction lives in toolwire so the chat parent does not
// import domain/goals.
type goalGlanceFunc func(ctx context.Context, sessionKey string) string

// NewGoalGlanceFunc builds the ambient goal glance from the process goal store.
func NewGoalGlanceFunc() goalGlanceFunc {
	return toolwire.NewGoalGlanceFunc()
}
