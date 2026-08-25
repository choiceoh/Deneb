package chat

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// SubagentNotifier routes completed child results back to their parent.
type SubagentNotifier = leafbind.SubagentNotifier

// SubagentNotifierDeps supplies parent-run callbacks without coupling the
// notifier package to the chat handler.
type SubagentNotifierDeps struct {
	Logger       *slog.Logger
	HasActiveRun func(sessionKey string) bool
	StartRun     func(reqID string, params RunParams, isSteer bool)
	EnqueuePend  func(sessionKey string, params RunParams)
	Sessions     func() *session.Manager
	// LastAssistantText reads a child's final assistant text when the session's
	// LastOutput is not populated yet (see leafbind.SubagentNotifierDeps).
	LastAssistantText func(sessionKey string) string
}

// NewSubagentNotifier creates and subscribes a child-completion notifier.
func NewSubagentNotifier(deps SubagentNotifierDeps) *SubagentNotifier {
	return leafbind.NewSubagentNotifier(leafbind.SubagentNotifierDeps{
		Logger:                 deps.Logger,
		HasActiveRun:           deps.HasActiveRun,
		StartRun:               deps.StartRun,
		EnqueuePend:            deps.EnqueuePend,
		Sessions:               deps.Sessions,
		LastAssistantText:      deps.LastAssistantText,
		Delivery:               deliveryFromSessionKey,
		ParentTerminatedReason: subagentParentTerminatedReason,
	})
}
