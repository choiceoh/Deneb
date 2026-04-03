package shadow

import (
	"fmt"
	"strings"
	"time"
)

// ErrorLearner tracks error patterns across sessions and provides
// historical context when similar errors recur.
type ErrorLearner struct {
	svc *Service

	// State (guarded by svc.mu).
	errorHistory []ErrorRecord
}

// ErrorRecord captures an error occurrence with context.
type ErrorRecord struct {
	Pattern     string `json:"pattern"` // normalized error signature
	Message     string `json:"message"` // original error message (truncated)
	SessionKey  string `json:"sessionKey"`
	OccurredAt  int64  `json:"occurredAt"`  // unix ms
	Resolution  string `json:"resolution"`  // how it was resolved (if known)
	Occurrences int    `json:"occurrences"` // how many times this pattern appeared
}

// ErrorInsight is returned when a recurring error is detected.
type ErrorInsight struct {
	Pattern        string `json:"pattern"`
	TotalCount     int    `json:"totalCount"`
	LastSeen       int64  `json:"lastSeen"`
	LastResolution string `json:"lastResolution,omitempty"`
	Suggestion     string `json:"suggestion"` // Korean hint
}

func newErrorLearner(svc *Service) *ErrorLearner {
	return &ErrorLearner{svc: svc}
}

// errorSignatures are patterns that indicate errors in conversation content.
var errorSignatures = []string{
	"error", "실패", "에러", "오류", "failed", "panic",
	"timeout", "타임아웃", "connection refused", "permission denied",
	"not found", "없습니다", "찾을 수 없", "빌드 실패",
	"test fail", "테스트 실패", "compile error", "컴파일",
}

// OnMessageForErrors scans a message for error patterns.
func (el *ErrorLearner) OnMessageForErrors(sessionKey, content string) {
	lower := strings.ToLower(content)
	for _, sig := range errorSignatures {
		if !strings.Contains(lower, strings.ToLower(sig)) {
			continue
		}

		pattern := normalizeErrorPattern(content, sig)
		el.recordError(pattern, content, sessionKey)
		return // one error per message
	}
}

// recordError tracks an error occurrence.
func (el *ErrorLearner) recordError(pattern, message, sessionKey string) {
	var recurringEvent *ShadowEvent

	el.svc.mu.Lock()
	// Check for existing pattern.
	for i := range el.errorHistory {
		if el.errorHistory[i].Pattern == pattern {
			el.errorHistory[i].Occurrences++
			el.errorHistory[i].OccurredAt = time.Now().UnixMilli()
			el.errorHistory[i].SessionKey = sessionKey

			// If this is a recurring error (3+), prepare event for after unlock.
			if el.errorHistory[i].Occurrences >= 3 {
				recurringEvent = &ShadowEvent{Type: "recurring_error", Payload: map[string]any{
					"pattern":     pattern,
					"occurrences": el.errorHistory[i].Occurrences,
				}}
			}
			el.svc.mu.Unlock()
			if recurringEvent != nil {
				el.svc.emit(*recurringEvent)
			}
			return
		}
	}

	// New error pattern.
	record := ErrorRecord{
		Pattern:     pattern,
		Message:     truncate(message, 300),
		SessionKey:  sessionKey,
		OccurredAt:  time.Now().UnixMilli(),
		Occurrences: 1,
	}
	el.errorHistory = append(el.errorHistory, record)

	// Cap at 300 records.
	if len(el.errorHistory) > 300 {
		el.errorHistory = el.errorHistory[len(el.errorHistory)-300:]
	}
	el.svc.mu.Unlock()
}

// OnCIFailure records a CI failure from a GitHub workflow_run webhook.
// Uses the same recurring-error escalation as conversation errors.
func (el *ErrorLearner) OnCIFailure(workflow, branch, url string) {
	pattern := fmt.Sprintf("CI:%s:%s", workflow, normalizeVariableParts(branch))
	message := fmt.Sprintf("CI 워크플로 '%s' 실패 (브랜치: %s)\n%s", workflow, branch, url)
	el.recordError(pattern, message, "github:ci")
}

// RecordResolution marks that an error was resolved (e.g., session went from failed to done).
func (el *ErrorLearner) RecordResolution(sessionKey, resolution string) {
	el.svc.mu.Lock()
	defer el.svc.mu.Unlock()

	// Find the most recent error from this session.
	for i := len(el.errorHistory) - 1; i >= 0; i-- {
		if el.errorHistory[i].SessionKey == sessionKey && el.errorHistory[i].Resolution == "" {
			el.errorHistory[i].Resolution = truncate(resolution, 200)
			return
		}
	}
}

// GetInsight returns insight for a given error pattern if it has been seen before.
func (el *ErrorLearner) GetInsight(content string) *ErrorInsight {
	lower := strings.ToLower(content)
	for _, sig := range errorSignatures {
		if !strings.Contains(lower, strings.ToLower(sig)) {
			continue
		}
		pattern := normalizeErrorPattern(content, sig)

		el.svc.mu.Lock()
		for _, r := range el.errorHistory {
			if r.Pattern == pattern && r.Occurrences >= 2 {
				insight := &ErrorInsight{
					Pattern:        pattern,
					TotalCount:     r.Occurrences,
					LastSeen:       r.OccurredAt,
					LastResolution: r.Resolution,
				}
				if r.Resolution != "" {
					insight.Suggestion = fmt.Sprintf("이 에러는 이전에 %d회 발생했고, 마지막 해결: %s",
						r.Occurrences, r.Resolution)
				} else {
					insight.Suggestion = fmt.Sprintf("이 에러는 이전에 %d회 발생했습니다 (해결 기록 없음)",
						r.Occurrences)
				}
				el.svc.mu.Unlock()
				return insight
			}
		}
		el.svc.mu.Unlock()
	}
	return nil
}

// GetErrorHistory returns recent error records.
func (el *ErrorLearner) GetErrorHistory() []ErrorRecord {
	el.svc.mu.Lock()
	defer el.svc.mu.Unlock()
	result := make([]ErrorRecord, len(el.errorHistory))
	copy(result, el.errorHistory)
	return result
}

// GetRecurringErrors returns errors that have occurred 3+ times.
func (el *ErrorLearner) GetRecurringErrors() []ErrorRecord {
	el.svc.mu.Lock()
	defer el.svc.mu.Unlock()
	var result []ErrorRecord
	for _, r := range el.errorHistory {
		if r.Occurrences >= 3 {
			result = append(result, r)
		}
	}
	return result
}

// normalizeErrorPattern creates a simplified signature from an error message.
func normalizeErrorPattern(content, matchedSig string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, strings.ToLower(matchedSig))
	if idx < 0 {
		return matchedSig
	}

	// Extract ~50 chars around the match and normalize.
	runes := []rune(content)
	runeIdx := len([]rune(content[:idx]))
	start := runeIdx
	if start > 10 {
		start -= 10
	}
	end := runeIdx + 50
	if end > len(runes) {
		end = len(runes)
	}

	snippet := strings.TrimSpace(string(runes[start:end]))
	// Remove variable parts (numbers, timestamps, IDs).
	snippet = normalizeVariableParts(snippet)
	return snippet
}

// normalizeVariableParts replaces variable content with placeholders.
func normalizeVariableParts(s string) string {
	var result strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			result.WriteRune('N')
		} else {
			result.WriteRune(r)
		}
	}
	// Collapse repeated N's.
	out := result.String()
	for strings.Contains(out, "NN") {
		out = strings.ReplaceAll(out, "NN", "N")
	}
	return out
}
