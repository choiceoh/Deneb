package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initCheckpointLifecycle subscribes to session lifecycle events so that the
// per-session checkpoint directory is released only once the session itself is
// deleted or evicted.
//
// Without this hook, abandoned checkpoint dirs linger until the 30-day
// startup GC pass (see server.go: checkpoint.CleanupStaleSessions). Heavy
// editing sessions can fill disk quickly, so this is the fast-reclaim path
// for sessions that are truly gone, while still preserving rollback history
// across normal run completion and /reset.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's blob dir is independent, and by
//     the time a delete event fires, the run goroutine has already exited and
//     no Snapshot calls remain (see run_start.go — the
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
// Only EventDeleted qualifies. Terminal runs and /reset must keep checkpoint
// history intact so checkpoint.{list,diff,restore} and /rollback continue to
// work after a run completes or is reset. Session deletion/GC is the actual
// end-of-life boundary for the persisted rollback data.
func shouldReleaseCheckpoints(e session.Event) bool {
	switch e.Kind {
	case session.EventDeleted:
		return true
	}
	return false
}
