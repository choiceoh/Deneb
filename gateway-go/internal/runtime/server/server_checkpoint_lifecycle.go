package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initCheckpointLifecycle subscribes to session lifecycle events so that the
// per-session checkpoint directory is released only when the user has truly
// abandoned it — via an explicit /reset, or when the session is deleted/GC'd.
//
// Terminal runs intentionally keep their checkpoints so /rollback still works
// after a successful or failed edit turn completes. Disk is reclaimed later by
// explicit reset/delete and the startup stale-session cleanup.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's blob dir is independent, and reset/
//     delete happen after the owning workflow has released the session.
//
// Called from registerSessionRPCMethods after SetCheckpointRoot; the
// returned unsubscribe handle is stored on ServerRPC and invoked on
// shutdown (see server_lifecycle.go doShutdown).
func (s *Server) initCheckpointLifecycle(root string) {
	if root == "" || s.sessions == nil {
		return
	}
	logger := s.logger
	s.checkpointLifecycleUnsub = s.sessions.EventBusRef().Subscribe(func(e session.Event) {
		if !shouldReleaseCheckpoints(e) {
			return
		}
		key := e.Key
		safego.GoWithSlog(logger, "checkpoint-session-end", func() {
			if err := checkpoint.RemoveSessionByID(root, key); err != nil {
				logger.Warn("failed to cleanup session checkpoints",
					"session", key,
					"event", string(e.Kind),
					"error", err)
			}
		})
	})
}

// shouldReleaseCheckpoints decides whether a session lifecycle event signals
// that the session's checkpoint directory can be reclaimed.
//
//  1. EventDeleted — session has been fully evicted (explicit delete or GC),
//     no resumption possible.
//  2. EventStatusChanged → empty status — that is a ResetSession emission
//     (see patch.go: ResetSession sets Status=""). /reset explicitly clears
//     the session's runtime state, and keeping the checkpoint dir around
//     after a reset defeats the user's intent.
//
// Terminal status transitions are intentionally ignored so rollback history
// survives ordinary run completion.
func shouldReleaseCheckpoints(e session.Event) bool {
	switch e.Kind {
	case session.EventDeleted:
		return true
	case session.EventStatusChanged:
		// /reset → NewStatus == "".
		return e.NewStatus == ""
	case session.EventCreated:
		return false
	}
	return false
}
