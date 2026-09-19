package server

import (
	"encoding/json"
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	runtimehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/health"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// handleHealth responds with gateway health status including subsystem state.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := s.collectBaseHealth()
	if manifest := s.runtimeManifestSnapshot(); manifest != nil {
		health["runtime_manifest"] = manifest
	}
	if propus, ok := runtimehealth.Propus(s.genesisTracker); ok {
		attachPropus(health, propus)
	}

	if s.fleet != nil {
		health["fleet"] = s.fleet.HealthReport()
	}

	// Prefix-cache and GPU telemetry are independent, optional probes. Collect
	// them concurrently so their individual two-second backstops do not add up
	// and make a healthy gateway miss the three-second self-poll deadline.
	optional := s.healthProbes.Collect(r.Context(), s.engineHealthDays)
	if optional.CachePresent {
		health["cache"] = optional.Cache
	}
	if optional.GPUPresent {
		health["gpu"] = optional.GPU
	}

	s.writeJSON(w, http.StatusOK, health)
}

// attachPropus publishes the canonical name and its legacy compatibility alias
// as the same immutable snapshot.
func attachPropus(health map[string]any, section *runtimehealth.PropusSection) {
	health["propus"] = section
	health["self_evolution"] = section
}

// engineHealthDays returns the newest day of the local engines' measured
// history for /health's prompt-cache section, or nil when no engine is
// configured or the maintenance suite is not built yet (early startup, tests).
func (s *Server) engineHealthDays() []enginespeed.DayStat {
	store := s.modelMaintenance.EngineSpeed()
	if store == nil {
		return nil
	}
	return store.Days(1)
}

// handleHealthGPU serves GET /health/gpu — the GPU telemetry section as a
// standalone endpoint for operators who want just the box's utilization / VRAM
// / temperature without the full /health payload. Returns
// {"gpu": null, "present": false} on a host without an NVIDIA GPU (200, not an
// error: "no GPU here" is a valid, queryable answer).
func (s *Server) handleHealthGPU(w http.ResponseWriter, r *http.Request) {
	stats, present := s.healthProbes.GPU(r.Context())
	out := map[string]any{"present": present}
	if present {
		out["gpu"] = stats
	}
	s.writeJSON(w, http.StatusOK, out)
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
		httputil.LogEncodeError(s.logger, "json encode error", err)
	}
}
