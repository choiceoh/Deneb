package server

import (
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/observatory"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// handleObservatory serves Deneb's self-improvement telemetry as one compact,
// machine-readable digest — so an agent (or an external puppeteer) reads its own
// status in a single call instead of spelunking a dozen files under ~/.deneb.
// Loopback-only, like the other introspection routes: it surfaces internal
// state, not the public app surface. Default is the LLM-readable markdown
// digest; ?format=json returns the structured report.
func (s *Server) handleObservatory(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	rep := observatory.Snapshot(config.ResolveStateDir(), time.Now())
	if r.URL.Query().Get("format") == "json" {
		s.writeJSON(w, http.StatusOK, rep)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(rep.Markdown()))
}
