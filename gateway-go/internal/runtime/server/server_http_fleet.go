package server

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Fleet passthrough: the native app manages the SparkFleet control plane (the
// sibling service that launches/monitors the GPU containers Deneb runs on)
// through the gateway, which is the only party that can reach it — the app may
// be off the tailnet, and SparkFleet's listener is Tailscale-bound. Requests
// are authenticated with the same client token as every miniapp route, then
// forwarded verbatim to DENEB_SPARKFLEET_URL.
//
//	{METHOD} /api/v1/fleet/{sparkfleet path}   (X-Deneb-Client-Token header)
//
// Only the allowlisted method+path pairs below pass. Deliberately excluded:
// SparkFleet's recipe *editing* endpoints (save/delete) — an editable recipe is
// arbitrary command execution on the fleet, which stays a dashboard-only,
// operator-at-a-real-browser action. Launching/stopping the version-controlled
// recipes is allowed; they are authorized by the recipe files themselves.

// fleetAllowGet / fleetAllowPost are the exact upstream paths the app may call.
var (
	fleetAllowGet = map[string]bool{
		"/api/state":     true,
		"/api/services":  true,
		"/api/config":    true,
		"/api/recipes":   true,
		"/api/jobs":      true,
		"/api/evals":     true,
		"/api/logs":      true,
		"/api/hf/search": true,
		"/api/hf/info":   true,
		"/api/hf/token":  true,
	}
	fleetAllowPost = map[string]bool{
		"/api/recipes/action":  true,
		"/api/recipes/reload":  true,
		"/api/control":         true,
		"/api/models/sync":     true,
		"/api/models/delete":   true,
		"/api/models/download": true,
		"/api/images/sync":     true,
		"/api/images/delete":   true,
		"/api/hf/token":        true,
		"/api/eval":            true,
	}
)

// fleetPathAllowed reports whether the upstream method+path is in the
// passthrough allowlist. /api/jobs/{id} is the one parameterized GET (single
// path segment — no further slashes).
func fleetPathAllowed(method, path string) bool {
	switch method {
	case http.MethodGet:
		if fleetAllowGet[path] {
			return true
		}
		if id, ok := strings.CutPrefix(path, "/api/jobs/"); ok {
			return id != "" && !strings.Contains(id, "/")
		}
	case http.MethodPost:
		return fleetAllowPost[path]
	}
	return false
}

// fleetProxyTimeout bounds one forwarded call. SparkFleet's mutating endpoints
// return a job id immediately (the work runs as a background job there), so
// nothing legitimate takes long; container logs are the slowest at ~20s.
const fleetProxyTimeout = 45 * time.Second

// fleetMaxBody caps a forwarded request body — mirrors SparkFleet's own cap.
const fleetMaxBody = 1 << 20

var fleetHTTP = httputil.NewClient(fleetProxyTimeout)

// handleFleetProxy authenticates the client token, then forwards the request.
func (s *Server) handleFleetProxy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappRequest(w, r); !ok {
		return
	}
	s.fleetProxy(w, r)
}

// fleetProxy forwards an allowlisted request to SparkFleet and relays the
// response verbatim (status, content type, body). Split from the auth wrapper
// so tests exercise the forwarding logic directly.
func (s *Server) fleetProxy(w http.ResponseWriter, r *http.Request) {
	base := s.fleet.BaseURL()
	if base == "" {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "fleet integration is off — set DENEB_SPARKFLEET_URL on the gateway",
		})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/fleet")
	if !fleetPathAllowed(r.Method, path) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "fleet path not allowed: " + r.Method + " " + path,
		})
		return
	}

	upstream := base + path
	if r.URL.RawQuery != "" {
		upstream += "?" + r.URL.RawQuery
	}
	body := http.MaxBytesReader(w, r.Body, fleetMaxBody)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, body)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "build fleet request: " + err.Error()})
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	// SparkFleet normally relies on its Tailscale-bound listener; pass its API
	// token when the operator has set one.
	if tok := strings.TrimSpace(os.Getenv("DENEB_SPARKFLEET_TOKEN")); tok != "" {
		req.Header.Set("X-Fleet-Token", tok)
	}

	resp, err := fleetHTTP.Do(req)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "sparkfleet unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.logger.Warn("fleet proxy: copy response", "error", err)
	}
}
