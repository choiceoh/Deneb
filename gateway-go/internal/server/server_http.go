package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	// maxRPCBodyBytes limits the HTTP RPC request body to 1 MB.
	maxRPCBodyBytes = 1 * 1024 * 1024
)

// handleMetrics responds with Prometheus-compatible text exposition of all metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.WriteMetrics(w)
}

// handleHealth responds with gateway health status including subsystem state.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	authMode := ""
	providerCount := 0
	if s.runtimeCfg != nil {
		authMode = s.runtimeCfg.AuthMode
	}
	if s.providers != nil {
		providerCount = len(s.providers.List())
	}

	// Count active processes.
	activeProcesses := 0
	if s.processes != nil {
		for _, p := range s.processes.List() {
			if p.Status == process.StatusRunning {
				activeProcesses++
			}
		}
	}

	// Count cron tasks.
	cronTasks := 0
	if s.cron != nil {
		cronTasks = len(s.cron.List())
	}

	// Count registered hooks.
	hooksCount := 0
	if s.hooks != nil {
		hooksCount = len(s.hooks.List())
	}

	// Channel health summary.
	channelHealthSummary := map[string]int{"healthy": 0, "unhealthy": 0}
	if s.channelHealth != nil {
		for _, ch := range s.channelHealth.HealthSnapshot() {
			if ch.Healthy {
				channelHealthSummary["healthy"]++
			} else {
				channelHealthSummary["unhealthy"]++
			}
		}
	}

	// Current model.
	currentModel := ""
	if s.chatHandler != nil {
		currentModel = s.chatHandler.DefaultModel()
	}
	if currentModel == "" && s.modelRegistry != nil {
		currentModel = s.modelRegistry.FullModelID(modelrole.RoleMain)
	}

	uptime := time.Since(s.startedAt)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"version":   s.version,
		"model":     currentModel,
		"uptime":    formatUptimeHTTP(uptime),
		"uptime_ms": uptime.Milliseconds(),
		"subsystems": map[string]any{
			"core": coreLabel(s.rustFFI),
			"vega": s.vegaBackend != nil,
			"auth": authMode,
		},
		"connections": s.clientCnt.Load(),
		"sessions":    s.sessions.Count(),
		"channels":    channelHealthSummary,
		"workers": map[string]int{
			"processes": activeProcesses,
			"cron":      cronTasks,
			"hooks":     hooksCount,
		},
		"providers": providerCount,
	})
}

// handleReady responds with readiness status.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	ready := s.ready.Load()
	httpStatus := http.StatusOK
	statusLabel := "ok"
	if !ready {
		httpStatus = http.StatusServiceUnavailable
		statusLabel = "unavailable"
	}
	s.writeJSON(w, httpStatus, map[string]any{
		"status": statusLabel,
		"ready":  ready,
	})
}

// writeJSON encodes v as JSON to the response writer, logging any encoding errors.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("json encode error", "error", err)
	}
}

// coreLabel returns a human-readable label for the core backend.
func coreLabel(rustFFI bool) string {
	if rustFFI {
		return "rust-ffi"
	}
	return "go"
}

// formatUptimeHTTP returns a human-readable uptime string for HTTP responses.
func formatUptimeHTTP(d time.Duration) string {
	d = d.Round(time.Second)
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	rs := s % 60
	if m < 60 {
		if rs == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, rs)
	}
	h := m / 60
	rm := m % 60
	if rm == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, rm)
}

// handleRPC processes HTTP JSON-RPC requests via the dispatcher.
// Extracts Bearer token from Authorization header for authentication.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// Track activity.
	if s.activity != nil {
		s.activity.Touch()
	}

	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
		ID     string          `json:"id"`
	}

	limited := http.MaxBytesReader(w, r.Body, maxRPCBodyBytes)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, rpcerr.InvalidRequest("invalid JSON").Response(""))
		return
	}

	if req.Method == "" || req.ID == "" {
		s.writeJSON(w, http.StatusBadRequest, rpcerr.New(protocol.ErrMissingParam, "method and id are required").Response(req.ID))
		return
	}

	// Resolve auth from Bearer token.
	role := ""
	authenticated := false
	var scopes []auth.Scope

	if s.authValidator != nil {
		token := extractBearerToken(r)
		if token != "" {
			claims, err := s.authValidator.ValidateToken(token)
			if err != nil {
				s.writeJSON(w, http.StatusUnauthorized, rpcerr.Unauthorized("invalid token: "+err.Error()).Response(req.ID))
				return
			}
			role = string(claims.Role)
			authenticated = true
			scopes = claims.Scopes
		}
	} else {
		// No-auth mode: treat all HTTP requests as operator.
		role = "operator"
		authenticated = true
		scopes = auth.DefaultScopes(auth.RoleOperator)
	}

	// Authorize method call.
	if authErr := rpc.AuthorizeMethod(req.Method, role, authenticated, scopes); authErr != nil {
		status := http.StatusForbidden
		if authErr.Code == protocol.ErrUnauthorized {
			status = http.StatusUnauthorized
		}
		s.writeJSON(w, status, protocol.NewResponseError(req.ID, authErr))
		return
	}

	frame := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     req.ID,
		Method: req.Method,
		Params: req.Params,
	}

	dispatchCtx, dispatchCancel := context.WithTimeout(r.Context(), timeouts.RPCDispatch)
	resp := s.dispatcher.Dispatch(dispatchCtx, frame)
	dispatchCancel()

	s.writeJSON(w, http.StatusOK, resp)
}

// extractBearerToken extracts the token from an "Authorization: Bearer <token>" header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return authHeader[len(prefix):]
	}
	return ""
}
