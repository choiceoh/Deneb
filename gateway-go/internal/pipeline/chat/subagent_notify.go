package chat

import (
	"log/slog"

	subagentnotify "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// SubagentNotifier routes completed child results back to their parent.
type SubagentNotifier = subagentnotify.SubagentNotifier

// SubagentNotifierDeps supplies parent-run callbacks without coupling the
// notifier package to the chat handler.
type SubagentNotifierDeps struct {
	Logger       *slog.Logger
	HasActiveRun func(sessionKey string) bool
	StartRun     func(reqID string, params RunParams, isSteer bool)
	EnqueuePend  func(sessionKey string, params RunParams)
	Sessions     func() *session.Manager
}

// NewSubagentNotifier creates and subscribes a child-completion notifier.
func NewSubagentNotifier(deps SubagentNotifierDeps) *SubagentNotifier {
	return subagentnotify.NewSubagentNotifier(subagentnotify.SubagentNotifierDeps{
		Logger:                 deps.Logger,
		HasActiveRun:           deps.HasActiveRun,
		StartRun:               deps.StartRun,
		EnqueuePend:            deps.EnqueuePend,
		Sessions:               deps.Sessions,
		Delivery:               deliveryFromSessionKey,
		ParentTerminatedReason: subagentParentTerminatedReason,
	})
}
