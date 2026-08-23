package server

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"

// handleFactJournalFailure is the process-fatal boundary for an append whose
// durable outcome cannot be proven. Serving the older in-memory revision after
// that point would be worse than downtime: the journal may already contain the
// next revision. Close readiness synchronously, discard every fact-derived
// prompt snapshot without approving a new revision, then enter the normal
// ordered shutdown so the supervisor can restart and replay/truncate the tail.
func (s *Server) handleFactJournalFailure(cause error) {
	if s == nil {
		return
	}
	chat.DisableFactDerivedCaches()
	s.ready.Store(false)
	if s.logger != nil {
		s.logger.Error("fact journal entered ambiguous state; restarting gateway", "error", cause)
	}
	go func() {
		if err := s.shutdown(); err != nil && s.logger != nil {
			s.logger.Error("fact journal fatal shutdown failed", "error", err)
		}
	}()
}
