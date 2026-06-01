package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initCheckpointLifecycle subscribes to session lifecycle events so that the
// per-session checkpoint directory is released when the session is explicitly
// discarded — via /reset or eventual session eviction/delete.
//
// Without this hook, abandoned checkpoint dirs linger until the 30-day
// startup GC pass (see server.go: checkpoint.CleanupStaleSessions). Reset and
// session-GC/delete are the fast-reclaim paths that do not break the user-
// visible post-run /rollback workflow.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's blob dir is independent, and by
//     the time a terminal/reset/delete event fires, the run goroutine has
//     already exited and no Snapshot calls remain (see run_start.go — the
//     Manager is scoped to runCtx and dropped on run completion).
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
// EventCreated and terminal/non-terminal status transitions are no-ops. We
// keep checkpoints after a run ends so /rollback can inspect and restore the
// just-finished edit history; the directory is reclaimed later when the
// session is reset or evicted.
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
