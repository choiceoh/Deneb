package shadow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MemoryConsolidator extracts important facts from conversations and prepares
// them for memory storage.
type MemoryConsolidator struct {
	svc *Service

	// State (guarded by svc.mu).
	extractedFacts []ExtractedFact
	messageBuffer  []bufferedMessage // recent messages for batch extraction
}

// ExtractedFact is an important fact extracted from conversation.
type ExtractedFact struct {
	Content     string `json:"content"`     // the fact itself
	Category    string `json:"category"`    // "preference", "technical", "personal", "project"
	Source      string `json:"source"`      // session key + approximate context
	ExtractedAt int64  `json:"extractedAt"` // unix ms
	Importance  string `json:"importance"`  // "high", "medium", "low"
}

type bufferedMessage struct {
	Role    string
	Content string
	Ts      int64
}

func newMemoryConsolidator(svc *Service) *MemoryConsolidator {
	return &MemoryConsolidator{svc: svc}
}

// factPatterns maps pattern keywords to fact categories.
var factPatterns = map[string]string{
	// Preferences.
	"좋아하": "preference", "선호하": "preference", "싫어하": "preference",
	"prefer": "preference", "always use": "preference",
	// Technical decisions.
	"결정했": "technical", "사용하기로": "technical", "패턴은": "technical",
	"architecture": "technical", "we decided": "technical",
	// Personal info.
	"내 이름": "personal", "my name": "personal",
	// Project info.
	"프로젝트": "project", "배포": "project", "릴리스": "project",
	"version": "project", "release": "project",
}

// OnMessageForMemory analyzes a message and extracts facts.
func (mc *MemoryConsolidator) OnMessageForMemory(sessionKey string, msg json.RawMessage) {
	var parsed struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil {
		return
	}
	if parsed.Content == "" {
		return
	}

	// Buffer message for batch analysis.
	mc.svc.mu.Lock()
	mc.messageBuffer = append(mc.messageBuffer, bufferedMessage{
		Role:    parsed.Role,
		Content: parsed.Content,
		Ts:      time.Now().UnixMilli(),
	})
	// Cap buffer at 50 messages.
	if len(mc.messageBuffer) > 50 {
		mc.messageBuffer = mc.messageBuffer[len(mc.messageBuffer)-50:]
	}
	mc.svc.mu.Unlock()

	// Real-time fact extraction for high-signal patterns.
	facts := extractFacts(parsed.Content, sessionKey)
	if len(facts) > 0 {
		mc.svc.mu.Lock()
		for _, f := range facts {
			if len(mc.extractedFacts) >= maxExtractedFacts {
				mc.extractedFacts = mc.extractedFacts[1:]
			}
			mc.extractedFacts = append(mc.extractedFacts, f)
		}
		mc.svc.mu.Unlock()

		for _, f := range facts {
			mc.svc.emit(ShadowEvent{Type: "fact_extracted", Payload: f})
		}
	}
}

// extractFacts scans content for important facts.
func extractFacts(content, sessionKey string) []ExtractedFact {
	lower := strings.ToLower(content)
	var facts []ExtractedFact
	seen := make(map[string]bool)

	for pattern, category := range factPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			extracted := extractContext(content, strings.Index(lower, strings.ToLower(pattern)), 150)
			if seen[extracted] {
				continue
			}
			seen[extracted] = true

			importance := "medium"
			if category == "preference" || category == "personal" {
				importance = "high"
			}

			facts = append(facts, ExtractedFact{
				Content:     extracted,
				Category:    category,
				Source:      sessionKey,
				ExtractedAt: time.Now().UnixMilli(),
				Importance:  importance,
			})
		}
	}
	return facts
}

// GetExtractedFacts returns extracted facts, optionally filtered by category.
func (mc *MemoryConsolidator) GetExtractedFacts(category string) []ExtractedFact {
	mc.svc.mu.Lock()
	defer mc.svc.mu.Unlock()
	var result []ExtractedFact
	for _, f := range mc.extractedFacts {
		if category == "" || f.Category == category {
			result = append(result, f)
		}
	}
	return result
}

// GetBufferSummary returns a summary of the buffered messages.
func (mc *MemoryConsolidator) GetBufferSummary() string {
	mc.svc.mu.Lock()
	msgs := make([]bufferedMessage, len(mc.messageBuffer))
	copy(msgs, mc.messageBuffer)
	mc.svc.mu.Unlock()

	if len(msgs) == 0 {
		return "대화 내역 없음"
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("최근 대화 %d건:\n", len(msgs)))
	for _, m := range msgs {
		preview := truncate(m.Content, 80)
		summary.WriteString(fmt.Sprintf("  [%s] %s\n", m.Role, preview))
	}
	return summary.String()
}

const maxExtractedFacts = 200
