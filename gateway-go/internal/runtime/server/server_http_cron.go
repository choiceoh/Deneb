package server

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
)

// handleCronRun triggers a cron job by Name, intended for local scripts
// (e.g. kakao → email pipeline) that call the gateway directly via curl.
//
// Body: {"name":"job-name"}
// Response:
//
//	200 {"status":"ok","jobId":"..."}
//	400 {"error":"name is required"}
//	403 {"error":"localhost only"}
//	404 {"error":"job not found"}
//	503 {"error":"cron service unavailable"}
//
// The job is launched asynchronously — the HTTP response returns as soon as
// the job is queued, not when it finishes.
//
// Auth: localhost-only. The gateway already binds loopback by default in
// production, but we re-check r.RemoteAddr defensively in case a future
// deployment binds a wider interface.
func (s *Server) handleCronRun(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "localhost only",
		})
		return
	}

	if s.cronService == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "cron service unavailable",
		})
		return
	}

	var req struct {
		Name string `json:"name"`
		Mode string `json:"mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid JSON body",
		})
		return
	}
	if req.Name == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "name is required",
		})
		return
	}

	job, lookupErr := s.cronService.JobByName(req.Name)
	if lookupErr != nil {
		// Surface store I/O or parse failures as 500 so a corrupt/unreadable
		// jobs.json doesn't look like a missing job to operators. The full
		// error is logged server-side; the client only sees a generic notice.
		s.logger.Error("cron job lookup failed", "name", req.Name, "error", lookupErr)
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "cron store unavailable",
		})
		return
	}
	if job == nil {
		s.writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "job not found",
		})
		return
	}

	// Fire-and-forget on the server's lifecycle context so the job survives
	// the HTTP request's cancellation but is still cancelled on graceful
	// shutdown. Service.EnqueueRun currently never returns a non-nil error
	// (failures are logged asynchronously inside the run loop) — the return
	// value is intentionally ignored here so a future signature change can't
	// silently leak internal error text to a localhost-only client.
	_ = s.cronService.EnqueueRun(s.ShutdownCtx(), job.ID, req.Mode)

	s.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"jobId":  job.ID,
	})
}

// isLoopbackRemote returns true if remoteAddr is on the loopback interface.
// Accepts the "host:port" form Go places on http.Request.RemoteAddr.
func isLoopbackRemote(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
