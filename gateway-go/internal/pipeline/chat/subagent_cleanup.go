// subagent_cleanup.go — Cascade cleanup of child sessions when their parent ends.
//
// Children spawned via sessions_spawn normally outlive individual parent turns
// (that is the async design: the parent goes idle, the child finishes later and
// the SubagentNotifier delivers the result). But two parent transitions mean the
// children's work is unwanted:
//
//   - parent KILLED: the user explicitly aborted the parent's work
//   - parent DELETED: the session (and its transcript context) no longer exists
//
// Without cleanup, running children keep burning local vLLM slots until their
// own timeout, and their completion events try to notify a parent that is gone.
// This module subscribes to the session event bus and, on those transitions,
// interrupts each running child's in-flight run and marks it killed.
package chat

import (
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// subagentParentTerminatedReason marks children killed by the cascade so the
// SubagentNotifier can tell bookkeeping kills apart from real results.
const subagentParentTerminatedReason = "parent session terminated"

// SubagentCleanupDeps holds the dependencies for the cascade cleanup listener.
type SubagentCleanupDeps struct {
	Logger   *slog.Logger
	Sessions func() *session.Manager
	// InterruptRun cancels all in-flight runs for a session key (context cancel
	// via the abort tracker). Status flips alone do not stop the goroutine.
	InterruptRun func(sessionKey string)
}

// StartSubagentCleanup subscribes the cascade listener to the session event bus.
// Returns the unsubscribe function.
func StartSubagentCleanup(deps SubagentCleanupDeps) func() {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	sm := deps.Sessions()
	return sm.EventBusRef().Subscribe(func(event session.Event) {
		parentEnded := event.Kind == session.EventDeleted ||
			(event.Kind == session.EventStatusChanged && event.NewStatus == session.StatusKilled)
		if !parentEnded {
			return
		}
		killOrphanedChildren(logger, sm, deps.InterruptRun, event.Key)
	})
}

// killOrphanedChildren interrupts and kills every running child of parentKey.
// Children that already reached a terminal state are left alone (their results
// remain inspectable via the subagents tool); children that never started a run
// (empty status) are skipped — the state machine only allows "" → RUNNING, and
// their imminent run will be cleaned up by the next parent-terminal event or GC.
//
// Killing a child that is itself a parent re-enters this listener through the
// bus (events are delivered asynchronously per subscriber), so kills cascade
// down spawn trees without recursion here.
func killOrphanedChildren(logger *slog.Logger, sm *session.Manager, interrupt func(string), parentKey string) {
	for _, c := range sm.List() {
		if c.SpawnedBy != parentKey || c.Status != session.StatusRunning {
			continue
		}
		now := time.Now().UnixMilli()
		c.Status = session.StatusKilled
		c.FailureReason = subagentParentTerminatedReason
		c.EndedAt = &now
		if c.StartedAt != nil {
			runtime := now - *c.StartedAt
			c.RuntimeMs = &runtime
		}
		c.UpdatedAt = now
		if err := sm.Set(c); err != nil {
			// The child raced into a terminal state on its own; nothing to kill.
			continue
		}
		if interrupt != nil {
			interrupt(c.Key)
		}
		logger.Info("killed orphaned subagent after parent ended",
			"child", abbreviateSession(c.Key),
			"parent", abbreviateSession(parentKey))
	}
}
