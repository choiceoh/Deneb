package server

import (
	"net/http"
	"net/url"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// handleSessionKill handles POST /sessions/{key}/kill — kills a running session.
// Mirrors src/gateway/session/session-kill-http.ts.
func (s *Server) handleSessionKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok": false, "error": map[string]string{"type": "method_not_allowed", "message": "POST required"},
		})
		return
	}

	// Authenticate.
	if !s.authorizeHTTP(w, r) {
		return
	}

	// Extract session key from URL path.
	rawKey := r.PathValue("key")
	sessionKey, err := url.PathUnescape(rawKey)
	if err != nil || sessionKey == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": map[string]string{"type": "invalid_request", "message": "invalid session key"},
		})
		return
	}

	// Look up session.
	sess := s.sessions.Get(sessionKey)
	if sess == nil {
		s.writeJSON(w, http.StatusNotFound, map[string]any{
			"ok": false, "error": map[string]string{"type": "not_found", "message": "session not found"},
		})
		return
	}

	// Kill: set status to killed.
	killed := false
	if sess.Status == session.StatusRunning {
		now := time.Now().UnixMilli()
		sess.Status = session.StatusKilled
		sess.EndedAt = &now
		if sess.StartedAt != nil {
			runtime := now - *sess.StartedAt
			sess.RuntimeMs = &runtime
		}
		sess.UpdatedAt = now
		s.sessions.Set(sess)
		killed = true

		// Emit lifecycle event.
		if s.gatewaySubs != nil {
			s.gatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: sessionKey,
				Reason:     "killed",
			})
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"killed": killed,
	})
}
