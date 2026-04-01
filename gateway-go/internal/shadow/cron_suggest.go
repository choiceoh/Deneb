package shadow

import (
	"fmt"
	"strings"
	"time"
)

// CronSuggester detects repetitive task patterns in conversation and suggests
// cron jobs to automate them.
type CronSuggester struct {
	svc *Service

	// State (guarded by svc.mu).
	suggestions     []CronSuggestion
	taskMentions    map[string][]int64 // normalized task → timestamps of mentions
}

// CronSuggestion is a proposed cron job based on detected patterns.
type CronSuggestion struct {
	ID          string `json:"id"`
	Task        string `json:"task"`        // what the cron should do
	Schedule    string `json:"schedule"`    // suggested cron expression
	Reason      string `json:"reason"`      // Korean explanation
	DetectedAt  int64  `json:"detectedAt"`  // unix ms
	MentionCount int   `json:"mentionCount"` // how many times the task was mentioned
	Status      string `json:"status"`      // "suggested", "accepted", "dismissed"
}

func newCronSuggester(svc *Service) *CronSuggester {
	return &CronSuggester{
		svc:          svc,
		taskMentions: make(map[string][]int64),
	}
}

// cronKeywords detect time-based repetitive patterns.
var cronKeywords = map[string]string{
	"매일":    "0 9 * * *",    // daily at 9am
	"매주":    "0 9 * * 1",    // weekly Monday 9am
	"매시간":   "0 * * * *",    // hourly
	"주기적으로":  "0 */4 * * *",  // every 4 hours
	"정기적으로":  "0 */4 * * *",
	"매번":    "0 */2 * * *",  // every 2 hours
	"every day":   "0 9 * * *",
	"every week":  "0 9 * * 1",
	"every hour":  "0 * * * *",
	"daily":       "0 9 * * *",
	"weekly":      "0 9 * * 1",
	"아침마다":   "0 9 * * *",    // every morning
	"저녁마다":   "0 18 * * *",   // every evening
}

// OnMessageForCron checks if a message mentions repetitive tasks.
func (cs *CronSuggester) OnMessageForCron(content string) {
	lower := strings.ToLower(content)

	for keyword, schedule := range cronKeywords {
		if !strings.Contains(lower, strings.ToLower(keyword)) {
			continue
		}

		// Extract the task context around the keyword.
		idx := strings.Index(lower, strings.ToLower(keyword))
		task := extractContext(content, idx, 100)
		normalized := normalizeTask(task)

		cs.svc.mu.Lock()
		cs.taskMentions[normalized] = append(cs.taskMentions[normalized], time.Now().UnixMilli())

		// Prune old mentions (keep last 30 days).
		thirtyDaysAgo := time.Now().UnixMilli() - 30*24*60*60*1000
		var recent []int64
		for _, ts := range cs.taskMentions[normalized] {
			if ts > thirtyDaysAgo {
				recent = append(recent, ts)
			}
		}
		cs.taskMentions[normalized] = recent

		// Suggest cron job if mentioned 2+ times.
		if len(recent) >= 2 {
			// Check if we already suggested this.
			alreadySuggested := false
			for _, s := range cs.suggestions {
				if normalizeTask(s.Task) == normalized {
					alreadySuggested = true
					s.MentionCount = len(recent)
					break
				}
			}

			if !alreadySuggested {
				suggestion := CronSuggestion{
					ID:           fmt.Sprintf("cron_suggest_%d", time.Now().UnixNano()),
					Task:         task,
					Schedule:     schedule,
					Reason:       fmt.Sprintf("'%s' 패턴으로 %d회 반복 감지", keyword, len(recent)),
					DetectedAt:   time.Now().UnixMilli(),
					MentionCount: len(recent),
					Status:       "suggested",
				}
				if len(cs.suggestions) >= 30 {
					cs.suggestions = cs.suggestions[1:]
				}
				cs.suggestions = append(cs.suggestions, suggestion)
				cs.svc.mu.Unlock()

				cs.svc.emit(ShadowEvent{Type: "cron_suggested", Payload: suggestion})
				cs.notifySuggestion(suggestion)
				return
			}
		}
		cs.svc.mu.Unlock()
		return
	}
}

func (cs *CronSuggester) notifySuggestion(s CronSuggestion) {
	cs.svc.mu.Lock()
	notifier := cs.svc.cfg.Notifier
	cs.svc.mu.Unlock()
	if notifier == nil {
		return
	}

	msg := fmt.Sprintf("⏰ 크론 작업 제안\n작업: %s\n스케줄: %s\n이유: %s",
		truncate(s.Task, 80), s.Schedule, s.Reason)

	go func() {
		ctx, cancel := cs.svc.notifyCtx()
		defer cancel()
		if err := notifier.Notify(ctx, msg); err != nil {
			cs.svc.cfg.Logger.Warn("shadow: cron suggestion notification failed", "error", err)
		}
	}()
}

// GetSuggestions returns pending cron suggestions.
func (cs *CronSuggester) GetSuggestions() []CronSuggestion {
	cs.svc.mu.Lock()
	defer cs.svc.mu.Unlock()
	var result []CronSuggestion
	for _, s := range cs.suggestions {
		if s.Status == "suggested" {
			result = append(result, s)
		}
	}
	return result
}

// DismissSuggestion marks a suggestion as dismissed.
func (cs *CronSuggester) DismissSuggestion(id string) bool {
	cs.svc.mu.Lock()
	defer cs.svc.mu.Unlock()
	for i := range cs.suggestions {
		if cs.suggestions[i].ID == id {
			cs.suggestions[i].Status = "dismissed"
			return true
		}
	}
	return false
}

// normalizeTask creates a simplified key from a task description.
func normalizeTask(task string) string {
	// Lowercase, trim, collapse whitespace.
	s := strings.ToLower(strings.TrimSpace(task))
	fields := strings.Fields(s)
	if len(fields) > 8 {
		fields = fields[:8]
	}
	return strings.Join(fields, " ")
}
