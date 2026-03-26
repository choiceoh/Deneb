package autoreply

import (
	"sync"
	"time"
)

const maxHistoryKeys = 1000

// HistoryEntry records a message in the conversation history.
type HistoryEntry struct {
	Role       string `json:"role"` // "user" or "assistant"
	Text       string `json:"text"`
	Timestamp  int64  `json:"timestamp"`
	SessionKey string `json:"sessionKey,omitempty"`
}

// HistoryTracker manages per-session conversation history with LRU eviction.
type HistoryTracker struct {
	mu      sync.Mutex
	entries map[string][]HistoryEntry
	order   []string // insertion order for LRU eviction
}

// NewHistoryTracker creates a new history tracker.
func NewHistoryTracker() *HistoryTracker {
	return &HistoryTracker{
		entries: make(map[string][]HistoryEntry),
	}
}

// Append adds a history entry for a session key.
func (h *HistoryTracker) Append(sessionKey string, entry HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.entries[sessionKey]; !exists {
		h.order = append(h.order, sessionKey)
	}
	entry.Timestamp = time.Now().UnixMilli()
	entry.SessionKey = sessionKey
	h.entries[sessionKey] = append(h.entries[sessionKey], entry)

	// Evict oldest if over capacity.
	for len(h.order) > maxHistoryKeys {
		oldest := h.order[0]
		h.order = h.order[1:]
		delete(h.entries, oldest)
	}
}

// Get returns the history for a session key.
func (h *HistoryTracker) Get(sessionKey string) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries := h.entries[sessionKey]
	if entries == nil {
		return nil
	}
	result := make([]HistoryEntry, len(entries))
	copy(result, entries)
	return result
}

// GetRecent returns the last N entries for a session key.
func (h *HistoryTracker) GetRecent(sessionKey string, n int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries := h.entries[sessionKey]
	if entries == nil || n <= 0 {
		return nil
	}
	if n >= len(entries) {
		result := make([]HistoryEntry, len(entries))
		copy(result, entries)
		return result
	}
	result := make([]HistoryEntry, n)
	copy(result, entries[len(entries)-n:])
	return result
}

// Clear removes all history for a session key.
func (h *HistoryTracker) Clear(sessionKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.entries, sessionKey)
	// Remove from order.
	for i, key := range h.order {
		if key == sessionKey {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

// Count returns the total number of tracked sessions.
func (h *HistoryTracker) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// BuildHistoryContext formats recent history entries as context for the agent.
func BuildHistoryContext(entries []HistoryEntry, maxEntries int) string {
	if len(entries) == 0 {
		return ""
	}
	start := 0
	if maxEntries > 0 && len(entries) > maxEntries {
		start = len(entries) - maxEntries
	}

	var result string
	for _, e := range entries[start:] {
		prefix := "User"
		if e.Role == "assistant" {
			prefix = "Assistant"
		}
		result += prefix + ": " + e.Text + "\n"
	}
	return result
}
