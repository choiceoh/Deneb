package server

import (
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
)

// collectBaseHealth builds the always-present portion of the /health contract.
// Optional probes and subsystem-specific extensions are attached by the HTTP
// handler after this cheap, in-process snapshot has been collected.
func (s *Server) collectBaseHealth() map[string]any {
	providerCount := 0
	if s.providers != nil {
		providerCount = len(s.providers.List())
	}

	activeProcesses := 0
	if s.processes != nil {
		for _, p := range s.processes.List() {
			if p.Status == process.StatusRunning {
				activeProcesses++
			}
		}
	}

	cronTasks := 0
	if s.cronService != nil {
		cronTasks = s.cronService.Status().TaskCount
	}

	currentModel := ""
	if s.chatHandler != nil {
		currentModel = s.chatHandler.DefaultModel()
	}
	if currentModel == "" && s.modelRegistry != nil {
		currentModel = s.modelRegistry.FullModelID(modelrole.RoleMain)
	}

	localAIStatus := "off"
	if s.localAIHub != nil {
		if s.localAIHub.IsHealthy() {
			localAIStatus = "ok"
		} else {
			localAIStatus = "unhealthy"
		}
	}

	embeddingStatus := "off"
	if s.embeddingClient != nil {
		if s.embeddingClient.IsHealthy() {
			embeddingStatus = "ok"
		} else {
			embeddingStatus = "unhealthy"
		}
	}

	subsystems := map[string]any{
		"core":      "go",
		"local_ai":  localAIStatus,
		"embedding": embeddingStatus,
	}
	if s.rerankerClient != nil {
		subsystems["reranker"] = map[string]any{
			"status":   "configured",
			"identity": s.rerankerClient.Identity(),
			"stats":    s.rerankerClient.Stats(),
		}
	}
	retrieval := map[string]any{}
	if s.mailStore != nil {
		retrieval["mail"] = map[string]any{
			"index":       s.mailStore.SemanticStatus(),
			"calibration": s.mailStore.SemanticCalibration(),
		}
	}
	if s.workFeedStore != nil {
		retrieval["workfeed"] = map[string]any{
			"index":       s.workFeedStore.SemanticStatus(),
			"calibration": s.workFeedStore.SemanticCalibration(),
		}
	}
	if len(retrieval) > 0 {
		subsystems["retrieval"] = retrieval
	}
	if value := s.mailIngestHealth.Load(); value != nil {
		if ingestHealth, ok := value.(mailIngestHealth); ok {
			if s.mailIngestQueueStats != nil {
				ingestHealth.Queue = s.mailIngestQueueStats()
			}
			subsystems["mail_ingest"] = ingestHealth
		} else {
			subsystems["mail_ingest"] = value
		}
	}

	uptime := time.Since(s.startedAt)
	health := map[string]any{
		"status":     "ok",
		"version":    s.version,
		"model":      currentModel,
		"uptime":     formatUptimeHTTP(uptime),
		"uptime_ms":  uptime.Milliseconds(),
		"subsystems": subsystems,
		"sessions":   s.sessions.Count(),
		// The deploy idle gate (scripts/deploy/wait-idle.sh) polls this before a
		// hot-swap: swapping while a turn runs turns the graceful drain into
		// minutes of downtime, so the deploy waits (bounded) for zero first.
		// Source: the chat AbortTracker — the exact population the drain waits
		// for (sync + streaming turns; nil before the session phase = 0).
		"activity": map[string]any{
			"active_turns": s.chatHandler.ActiveRunCount(),
		},
		"workers": map[string]int{
			"processes": activeProcesses,
			"cron":      cronTasks,
		},
		"providers": providerCount,
	}
	if s.dispatcher != nil {
		health["rpc"] = map[string]any{
			"worker_pool": s.dispatcher.PoolStats(),
		}
	}
	return health
}

// formatUptimeHTTP returns a human-readable uptime string for HTTP responses.
func formatUptimeHTTP(d time.Duration) string {
	d = d.Round(time.Second)
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	remainingSeconds := seconds % 60
	if minutes < 60 {
		if remainingSeconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, remainingSeconds)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remainingMinutes)
}
