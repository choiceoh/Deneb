package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initSpilloverLifecycle subscribes to session lifecycle events so that any
// disk-backed tool-result spill files belonging to a session are deleted on
// explicit reset or full session eviction.
//
// Ordinary completed turns already clean their spillover in finishRun; this
// hook exists to catch reset/delete paths and any future lifecycle flows that
// bypass finishRun. Restricting the subscriber to those teardown paths avoids
// treating non-reset EventStatusChanged emissions (for example sessions.patch)
// as a destructive cleanup signal.
//
// The removal is fire-and-forget in a safego goroutine:
//   - Disk I/O is not worth blocking the event dispatcher for.
//   - Failure is non-user-facing (just wasted disk), per logging.md: Warn.
//   - Concurrency is safe: each session's files are independent, and
//     RemoveSession is idempotent — concurrent cleanup remains harmless.
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

// shouldReleaseSpillover mirrors shouldReleaseCheckpoints: only explicit reset
// (non-empty OldStatus -> empty NewStatus) or full session deletion trigger
// event-driven cleanup. Normal turn completion is handled in finishRun.
func shouldReleaseSpillover(e session.Event) bool {
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
