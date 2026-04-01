package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// taskPatterns are Korean and English phrases that indicate a pending task.
var taskPatterns = []string{
	"나중에", "TODO", "할 일", "다음에", "잊지 말고", "해야",
	"remind me", "later", "todo", "FIXME", "fixme",
	"기억해", "메모", "잊지마",
}

// analyzeMessage processes a transcript message and extracts insights.
func (s *Service) analyzeMessage(sessionKey string, msg json.RawMessage) {
	var parsed struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil {
		return
	}
	// Only analyze user and assistant messages with text content.
	if parsed.Content == "" || (parsed.Role != "user" && parsed.Role != "assistant") {
		return
	}

	// Task detection.
	tasks := detectTasks(parsed.Content, sessionKey)
	if len(tasks) > 0 {
		s.mu.Lock()
		for _, t := range tasks {
			if len(s.pendingTasks) >= maxPendingTasks {
				// Evict oldest completed/dismissed task first, else oldest pending.
				evicted := false
				for i, existing := range s.pendingTasks {
					if existing.Status != "pending" {
						s.pendingTasks = append(s.pendingTasks[:i], s.pendingTasks[i+1:]...)
						evicted = true
						break
					}
				}
				if !evicted {
					s.pendingTasks = s.pendingTasks[1:]
				}
			}
			s.pendingTasks = append(s.pendingTasks, t)
		}
		s.mu.Unlock()

		for _, t := range tasks {
			s.emit(ShadowEvent{Type: "task_detected", Payload: t})
		}

		s.cfg.Logger.Info("shadow: tasks detected",
			"count", len(tasks),
			"session", sessionKey,
		)
	}
}

// detectTasks scans message content for TODO-like patterns and returns
// TrackedTask entries for each detected mention.
func detectTasks(content, sessionKey string) []TrackedTask {
	lower := strings.ToLower(content)
	var tasks []TrackedTask
	seen := make(map[string]bool) // dedup by extracted content

	for _, pattern := range taskPatterns {
		idx := strings.Index(lower, strings.ToLower(pattern))
		if idx < 0 {
			continue
		}

		// Extract surrounding context (up to 120 chars around the match).
		extracted := extractContext(content, idx, 120)
		if seen[extracted] {
			continue
		}
		seen[extracted] = true

		tasks = append(tasks, TrackedTask{
			ID:         fmt.Sprintf("task_%d", time.Now().UnixNano()),
			Content:    extracted,
			DetectedAt: time.Now().UnixMilli(),
			SessionKey: sessionKey,
			Status:     "pending",
		})
	}
	return tasks
}

// extractContext extracts a substring of up to maxLen runes centered around idx.
func extractContext(s string, byteIdx, maxLen int) string {
	runes := []rune(s)
	// Convert byte index to approximate rune index.
	runeIdx := len([]rune(s[:byteIdx]))

	start := runeIdx - maxLen/2
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(runes) {
		end = len(runes)
		start = end - maxLen
		if start < 0 {
			start = 0
		}
	}

	result := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		result = "..." + result
	}
	if end < len(runes) {
		result = result + "..."
	}
	return result
}

// checkHealthIndicators processes session lifecycle events for health monitoring.
func (s *Service) checkHealthIndicators(event session.Event) {
	now := time.Now().UnixMilli()

	switch event.NewStatus {
	case session.StatusFailed:
		alert := HealthAlert{
			Type:    "session_failed",
			Message: fmt.Sprintf("세션 실패: %s → %s", event.OldStatus, event.NewStatus),
			Ts:      now,
		}
		s.addAlert(alert)
		s.trackFailure(now)
		s.notifyHealth(alert)

	case session.StatusTimeout:
		alert := HealthAlert{
			Type:    "session_timeout",
			Message: fmt.Sprintf("세션 타임아웃: %s", event.Key),
			Ts:      now,
		}
		s.addAlert(alert)
		s.trackFailure(now)
		s.notifyHealth(alert)
	}
}

// addAlert appends a health alert, capping at maxHealthAlerts.
func (s *Service) addAlert(alert HealthAlert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.healthAlerts) >= maxHealthAlerts {
		s.healthAlerts = s.healthAlerts[1:]
	}
	s.healthAlerts = append(s.healthAlerts, alert)
	s.emit(ShadowEvent{Type: "health_alert", Payload: alert})
}

// trackFailure records a failure timestamp for repeated-failure detection.
func (s *Service) trackFailure(ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	oneHourAgo := ts - int64(time.Hour/time.Millisecond)

	// Prune old failures.
	var recent []int64
	for _, ft := range s.recentFailures {
		if ft > oneHourAgo {
			recent = append(recent, ft)
		}
	}
	recent = append(recent, ts)
	s.recentFailures = recent

	// Escalate on 3+ failures in the last hour.
	if len(recent) >= 3 {
		alert := HealthAlert{
			Type:    "repeated_failures",
			Message: fmt.Sprintf("1시간 내 %d회 세션 실패 감지", len(recent)),
			Ts:      ts,
		}
		if len(s.healthAlerts) >= maxHealthAlerts {
			s.healthAlerts = s.healthAlerts[1:]
		}
		s.healthAlerts = append(s.healthAlerts, alert)
		s.notifyHealth(alert)

		// Reset to avoid repeated escalation.
		s.recentFailures = nil
	}
}

// notifyHealth sends a Telegram notification for a health alert.
func (s *Service) notifyHealth(alert HealthAlert) {
	s.mu.Lock()
	notifier := s.cfg.Notifier
	s.mu.Unlock()
	if notifier == nil {
		return
	}

	var emoji string
	switch alert.Type {
	case "session_failed":
		emoji = "❌"
	case "session_timeout":
		emoji = "⏱️"
	case "repeated_failures":
		emoji = "🚨"
	default:
		emoji = "⚠️"
	}

	msg := fmt.Sprintf("%s Shadow 알림: %s", emoji, alert.Message)
	go func() {
		ctx, cancel := context.WithTimeout(s.svcCtx, 15*time.Second)
		defer cancel()
		if err := notifier.Notify(ctx, msg); err != nil {
			s.cfg.Logger.Warn("shadow health notification failed", "error", err)
		}
	}()
}
