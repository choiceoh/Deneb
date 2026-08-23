package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
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

// shouldReleaseSpillover fires only when the session itself ends: /reset
// (empty NewStatus) or full deletion, which also covers GC eviction
// (domain/session/manager.go emits EventDeleted for evicted sessions).
//
// It deliberately does NOT mirror shouldReleaseCheckpoints on terminal status.
// A terminal status is a *run* outcome (DONE/FAILED/KILLED/TIMEOUT) that every
// ordinary turn reaches while the session lives on. Releasing there deleted a
// spill as soon as its producing turn finished, while compaction kept telling
// the model the full output was "still available via read_spillover(…)" —
// so every such pointer dangled from the next turn onward. Checkpoints can
// afford run-scoped release; spill handles cannot, because they are quoted
// back to the model in surviving history.
//
// Disk is still bounded: a session that ends releases here, and one that
// disappears without an event is collected by the TTL sweep, which skips only
// sessions the manager still reports as live (ai/agent/spillover.go).
func shouldReleaseSpillover(e session.Event) bool {
	switch e.Kind {
	case session.EventDeleted:
		return true
	case session.EventStatusChanged:
		return e.NewStatus == "" // /reset
	case session.EventCreated:
		return false
	}
	return false
}
