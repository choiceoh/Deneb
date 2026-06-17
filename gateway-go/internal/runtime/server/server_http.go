package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
)

// handleHealth responds with gateway health status including subsystem state.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	providerCount := 0
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
	if s.cronService != nil {
		cronTasks = s.cronService.Status().TaskCount
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

	// Local AI subsystem health.
	localAIStatus := "off"
	if s.localAIHub != nil {
		if s.localAIHub.IsHealthy() {
			localAIStatus = "ok"
		} else {
			localAIStatus = "unhealthy"
		}
	}

	// Embedding server health.
	embeddingStatus := "off"
	if s.embeddingClient != nil {
		if s.embeddingClient.IsHealthy() {
			embeddingStatus = "ok"
		} else {
			embeddingStatus = "unhealthy"
		}
	}

	uptime := time.Since(s.startedAt)
	subsystems := map[string]any{
		"core":      "go",
		"local_ai":  localAIStatus,
		"embedding": embeddingStatus,
	}
	if v := s.mailIngestHealth.Load(); v != nil {
		if mh, ok := v.(mailIngestHealth); ok {
			if s.mailIngestQueueStats != nil {
				mh.Queue = s.mailIngestQueueStats()
			}
			subsystems["mail_ingest"] = mh
		} else {
			subsystems["mail_ingest"] = v
		}
	}

	health := map[string]any{
		"status":     "ok",
		"version":    s.version,
		"model":      currentModel,
		"uptime":     formatUptimeHTTP(uptime),
		"uptime_ms":  uptime.Milliseconds(),
		"subsystems": subsystems,
		"sessions":   s.sessions.Count(),
		"channels":   channelHealthSummary,
		"workers": map[string]int{
			"processes": activeProcesses,
			"cron":      cronTasks,
		},
		"providers": providerCount,
	}

	// Self-evolution liveness: makes silent death of the skill loop visible.
	// If review_age keeps growing, the nudger→review→evolve pipeline stalled.
	if s.genesisTracker != nil {
		live := s.genesisTracker.LivenessSnapshot()
		se := map[string]any{
			"last_review_ms":        live.LastReviewAt,
			"last_review_ok":        live.LastReviewOK,
			"last_evolve_ms":        live.LastEvolveAt,
			"last_genesis_ms":       live.LastGenesisAt,
			"review_attempts":       live.ReviewAttempts,
			"review_skips":          live.ReviewSkips,
			"validation_rejections": live.ValidationRejections,
		}
		if live.LastReviewAt > 0 {
			se["review_age"] = formatUptimeHTTP(time.Since(time.UnixMilli(live.LastReviewAt)))
		}
		if live.LastError != "" {
			se["last_error"] = live.LastError
		}
		// Productivity/thrash signals so a loop that burns its budget re-evolving
		// one skill is visible here instead of only in the logs (PR #2328).
		eh := s.genesisTracker.EvolutionHealth()
		se["evolves_7d"] = eh.Evolves7d
		se["evolve_rejected_7d"] = eh.EvolveRejected7d
		se["evolve_rolled_back_7d"] = eh.EvolveRolledBack7d
		se["genesis_7d"] = eh.Genesis7d
		se["distinct_skills_evolved_7d"] = eh.DistinctSkillsEvolved7d
		if eh.TopEvolvedSkill != "" {
			se["top_evolved_skill"] = eh.TopEvolvedSkill
			se["top_evolved_count"] = eh.TopEvolvedCount
		}
		if eh.LastRejectedSkill != "" {
			se["last_rejected_skill"] = eh.LastRejectedSkill
			se["last_rejected_reason"] = eh.LastRejectedReason
		}
		if eh.Thrash {
			se["thrash"] = true
		}
		if usageQuality, err := s.genesisTracker.UsageQualitySummary(""); err == nil {
			se["usage_records"] = usageQuality.TotalRecords
			se["usage_counted_records"] = usageQuality.CountedRecords
			se["ignored_usage_records"] = usageQuality.IgnoredRecords
			if usageQuality.IgnoredUnactionableLegacyFailures > 0 {
				se["ignored_unactionable_legacy_failures"] = usageQuality.IgnoredUnactionableLegacyFailures
				se["top_ignored_unactionable_legacy_failure_skill"] = usageQuality.TopIgnoredUnactionableLegacyFailureSkill
				se["top_ignored_unactionable_legacy_failure_skill_count"] = usageQuality.TopIgnoredUnactionableLegacyFailureSkillCount
			}
		}
		if validationCases, err := s.genesisTracker.ValidationCaseSummary(""); err == nil {
			se["validation_case_records"] = validationCases.RawRecords
			se["validation_cases_unique"] = validationCases.UniqueRecords
			se["validation_case_duplicates"] = validationCases.DuplicateRecords
			se["validation_cases_automatic"] = validationCases.AutomaticRecords
			se["validation_cases_unique_automatic"] = validationCases.UniqueAutomaticRecords
			se["validation_case_skills"] = validationCases.SkillsWithCases
			if validationCases.WeakAutomaticRecords > 0 {
				se["validation_cases_weak_automatic"] = validationCases.WeakAutomaticRecords
				se["validation_cases_unique_weak_automatic"] = validationCases.UniqueWeakAutomaticCases
			}
			if validationCases.TopSkill != "" {
				se["validation_case_top_skill"] = validationCases.TopSkill
				se["validation_case_top_skill_cases"] = validationCases.TopSkillUniqueCases
			}
		}
		// Skill-library value: how many self-generated skills earn their keep.
		// Many unused = net-negative cost (catalog + prompt tokens, no payoff).
		if agentSkills, unused := s.genesisTracker.AgentSkillValueSummary(); agentSkills > 0 {
			se["agent_skills"] = agentSkills
			se["unused_agent_skills"] = unused
		}
		health["self_evolution"] = se
	}

	if s.fleet != nil {
		health["fleet"] = s.fleet.HealthReport()
	}

	s.writeJSON(w, http.StatusOK, health)
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
