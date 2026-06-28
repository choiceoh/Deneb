package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initCheckpointLifecycle subscribes to session lifecycle events so that the
// per-session checkpoint directory is released only when the session is
// explicitly reset or deleted.
//
// Checkpoints back the user-facing /rollback flow, which is expected to work
// across ordinary completed turns within the same session. Reclaiming on every
// terminal run status would erase the just-created history before the user can
// inspect or restore it on the next turn. Fast reclamation is therefore limited
// to explicit session teardown (/reset or deletion), while eventual GC still
// covers abandoned dirs.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's blob dir is independent, and by
//     the time a reset/delete event fires, the run goroutine has already
//     exited and no Snapshot calls remain (see run_start.go — the Manager is
//     scoped to runCtx and dropped on run completion).
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
//  2. EventStatusChanged from a non-empty status to empty — that is the
//     ResetSession emission (see patch.go: ResetSession sets Status="").
//
// Ordinary status transitions (running → done/failed/...) do NOT release:
// /rollback is scoped to the session, not to a single turn, so the next user
// turn must still be able to inspect and restore snapshots created earlier.
//
// EventCreated and ordinary status transitions are no-ops.
func shouldReleaseCheckpoints(e session.Event) bool {
	switch e.Kind {
	case session.EventDeleted:
		return true
	case session.EventStatusChanged:
		return e.OldStatus != "" && e.NewStatus == ""
	case session.EventCreated:
		return false
	}
	return false
}
