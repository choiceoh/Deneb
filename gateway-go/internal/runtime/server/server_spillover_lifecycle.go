package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initSpilloverLifecycle subscribes to session lifecycle events so that any
// disk-backed tool-result spill files belonging to a session are deleted the
// moment the session ends — terminal run status, explicit /reset, or full
// session eviction.
//
// Without this hook, spill files linger until the 30-minute TTL sweep in
// SpilloverStore.StartCleanup (see spillover.go). That window is fine for
// active agents but wastes disk when a user kills a long chain of large
// `exec` / `read` outputs and never returns to the session. We already call
// CleanSession from finishRun for the common path, but an event-driven hook
// also catches GC-evicted sessions and any future lifecycle paths that
// bypass finishRun.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's files are independent, and
//     RemoveSession is idempotent — concurrent CleanSession from finishRun
//     is harmless.
//
// Called from registerSessionRPCMethods after the spillover store is wired on
// s.toolDeps (see chat_pipeline.go); the returned unsubscribe handle is
// stored on ServerRPC and invoked on shutdown (see server_lifecycle.go
// doShutdown).
func (s *Server) initSpilloverLifecycle(store *agent.SpilloverStore) {
	if store == nil || s.sessions == nil {
		return
	}
	logger := s.logger
	s.spilloverLifecycleUnsub = s.sessions.EventBusRef().Subscribe(func(e session.Event) {
		if !shouldReleaseSpillover(e) {
			return
		}
		key := e.Key
		safego.GoWithSlog(logger, "spillover-session-end", func() {
			if err := store.RemoveSession(key); err != nil {
				logger.Warn("failed to cleanup session spillover",
					"session", key,
					"event", string(e.Kind),
					"error", err)
			}
		})
	})
}

// shouldReleaseSpillover mirrors shouldReleaseCheckpoints: only terminal
// status transitions, /reset (empty NewStatus), or full session deletion
// trigger cleanup. See server_checkpoint_lifecycle.go for the full rationale.
func shouldReleaseSpillover(e session.Event) bool {
	switch e.Kind {
	case session.EventDeleted:
		return true
	case session.EventStatusChanged:
		if e.NewStatus == "" {
			return true
		}
		return session.IsTerminal(e.NewStatus)
	case session.EventCreated:
		return false
	}
	return false
}
