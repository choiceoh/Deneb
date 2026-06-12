package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleFleetHook receives SparkFleet's webhook alerts (its `generic` notify
// dialect: {"source":"sparkfleet","level":"ok|warn|bad","title":...,
// "message":...}) and fans them straight out to connected native clients as a
// push notification — node down/up, container restart loops, job done/failed,
// low unified-memory headroom land on the operator's phone instead of only in
// a log nobody is watching (the 2026-06-12 silent SparkFleet death is the
// motivating incident).
//
// Deterministic by design: no agent judgment turn in the loop — an
// operational alert must arrive verbatim and immediately. Auth matches
// /api/event/ingest: loopback-only (SparkFleet runs on this same host and
// posts to 127.0.0.1; the gateway may bind wider). Delivery is best-effort
// SSE fan-out, same as every proactive push.
func (s *Server) handleFleetHook(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	var ev struct {
		Source  string `json:"source"`
		Level   string `json:"level"` // ok | warn | bad
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(ev.Title) == "" && strings.TrimSpace(ev.Message) == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "title or message required"})
		return
	}

	badge := "🛰"
	switch ev.Level {
	case "bad":
		badge = "🔴"
	case "warn":
		badge = "⚠️"
	}
	title := strings.TrimSpace(badge + " 플릿 · " + ev.Title)
	if s.pushHub != nil {
		s.pushHub.publish(clientPushEvent{Title: title, Body: ev.Message})
	}
	s.logger.Info("fleet alert relayed to clients", "level", ev.Level, "title", ev.Title)
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
