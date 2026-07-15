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
	if cron := s.CronService(); cron != nil {
		cronTasks = cron.Status().TaskCount
	}

	currentModel := ""
	if ch := s.ChatHandler(); ch != nil {
		currentModel = ch.DefaultModel()
	}
	if currentModel == "" {
		if mr := s.ModelRegistry(); mr != nil {
			currentModel = mr.FullModelID(modelrole.RoleMain)
		}
	}

	localAIStatus := "off"
	if s.Chat != nil && s.Chat.LocalAIHub != nil {
		if s.Chat.LocalAIHub.IsHealthy() {
			localAIStatus = "ok"
		} else {
			localAIStatus = "unhealthy"
		}
	}

	embeddingStatus := "off"
	if s.Chat != nil && s.Chat.EmbeddingClient != nil {
		if s.Chat.EmbeddingClient.IsHealthy() {
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
	if s.Chat != nil {
		if value := s.Chat.MailIngestHealthSnapshot(); value != nil {
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
		"sessions":   s.Sessions().Count(),
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
