package autoreply

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Multilingual abort trigger words. Matches the TS ABORT_TRIGGERS set.
var abortTriggers = map[string]bool{
	// English
	"stop": true, "esc": true, "abort": true, "cancel": true, "halt": true,
	"quit": true, "exit": true, "end": true, "kill": true, "break": true,
	// Korean
	"중지": true, "취소": true, "멈춰": true, "그만": true, "정지": true, "끝": true,
	// Chinese
	"停止": true, "取消": true, "停": true,
	// Japanese
	"やめて": true, "中止": true, "止めて": true, "ストップ": true,
	// Hindi
	"रुको": true, "बंद": true, "रोको": true,
	// Arabic
	"توقف": true, "إلغاء": true,
	// Russian
	"стоп": true, "отмена": true,
	// German
	"stopp": true, "abbrechen": true,
	// French
	"arrêt": true, "arrête": true, "annuler": true,
	// Spanish
	"para": true, "parar": true, "detener": true,
	// Portuguese
	"cancelar": true,
}

// IsAbortTrigger returns true if the text is a recognized abort trigger word.
func IsAbortTrigger(text string) bool {
	normalized := normalizeAbortText(text)
	if normalized == "" {
		return false
	}
	return abortTriggers[normalized]
}

// IsAbortRequestText returns true if the text is a /stop command or abort trigger.
func IsAbortRequestText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lowered := strings.ToLower(trimmed)
	if lowered == "/stop" || lowered == "/cancel" || lowered == "/abort" {
		return true
	}
	return IsAbortTrigger(trimmed)
}

func normalizeAbortText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	// Strip leading punctuation and emoji.
	stripped := strings.TrimLeftFunc(trimmed, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r) || r == '!' || r == '.'
	})
	stripped = strings.TrimRightFunc(stripped, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r) || r == '!' || r == '.'
	})
	return strings.ToLower(strings.TrimSpace(stripped))
}

// AbortMemory tracks recently aborted sessions to avoid re-triggering.
type AbortMemory struct {
	mu      sync.Mutex
	entries map[string]int64 // sessionKey -> timestamp
	maxSize int
}

// NewAbortMemory creates a new abort memory with the given max size.
func NewAbortMemory(maxSize int) *AbortMemory {
	if maxSize <= 0 {
		maxSize = 2000
	}
	return &AbortMemory{
		entries: make(map[string]int64, maxSize),
		maxSize: maxSize,
	}
}

// Record marks a session as recently aborted.
func (m *AbortMemory) Record(sessionKey string, ts int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Evict oldest if at capacity.
	if len(m.entries) >= m.maxSize {
		var oldestKey string
		var oldestTs int64
		for k, v := range m.entries {
			if oldestTs == 0 || v < oldestTs {
				oldestKey = k
				oldestTs = v
			}
		}
		if oldestKey != "" {
			delete(m.entries, oldestKey)
		}
	}
	m.entries[sessionKey] = ts
}

// WasRecentlyAborted returns true if the session was recently aborted.
func (m *AbortMemory) WasRecentlyAborted(sessionKey string, windowMs int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, ok := m.entries[sessionKey]
	if !ok {
		return false
	}
	now := currentTimeMs()
	return now-ts < windowMs
}

// Clear removes a session from abort memory.
func (m *AbortMemory) Clear(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, sessionKey)
}

var currentTimeMs = func() int64 {
	return time.Now().UnixMilli()
}
