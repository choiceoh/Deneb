package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// subagentNotifier routes completed child results back to their parent.
type subagentNotifier = leafbind.SubagentNotifier

// subagentNotifierDeps supplies parent-run callbacks without coupling the
// notifier package to the chat handler.
type subagentNotifierDeps struct {
	Logger       *slog.Logger
	HasActiveRun func(sessionKey string) bool
	StartRun     func(reqID string, params runParams, isSteer bool)
	EnqueuePend  func(sessionKey string, params runParams)
	Sessions     func() *session.Manager
}

// newSubagentNotifier creates and subscribes a child-completion notifier.
func newSubagentNotifier(deps subagentNotifierDeps) *subagentNotifier {
	return leafbind.NewSubagentNotifier(leafbind.SubagentNotifierDeps{
		Logger:                 deps.Logger,
		HasActiveRun:           deps.HasActiveRun,
		StartRun:               deps.StartRun,
		EnqueuePend:            deps.EnqueuePend,
		Sessions:               deps.Sessions,
		Delivery:               deliveryFromSessionKey,
		ParentTerminatedReason: subagentParentTerminatedReason,
	})
}
