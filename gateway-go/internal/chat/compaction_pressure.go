// compaction_pressure.go — Branch prediction for context overflow.
//
// CPU architecture analogy: branch prediction monitors token budget utilization
// and signals when compaction is likely needed in the next 1-2 runs. This allows
// the pipeline to pre-evaluate compaction metadata during the parallel prep phase,
// avoiding the expensive pattern of: LLM call fails → compaction → retry.
//
// Without prediction, a context overflow wastes the entire first LLM call (5-60s)
// before compaction begins. With prediction, the pipeline can pre-check and
// potentially pre-compact before sending to the LLM.
package chat

import (
	"log/slog"
	"sync"
)

// CompactionPressure tracks token budget utilization for branch prediction.
type CompactionPressure struct {
	mu sync.Mutex
	// sessions tracks the last known pressure per session key.
	sessions map[string]*pressureEntry
}

type pressureEntry struct {
	estimatedTokens int
	tokenBudget     int
	messageCount    int
}

// NewCompactionPressure creates a new compaction pressure monitor.
func NewCompactionPressure() *CompactionPressure {
	return &CompactionPressure{
		sessions: make(map[string]*pressureEntry),
	}
}

// pressureThreshold is the utilization ratio above which we predict overflow.
// At 80%, the next few messages are likely to push over the budget.
const pressureThreshold = 0.80

// Update records the current token utilization for a session after context assembly.
func (cp *CompactionPressure) Update(sessionKey string, estimatedTokens, tokenBudget, messageCount int) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.sessions[sessionKey] = &pressureEntry{
		estimatedTokens: estimatedTokens,
		tokenBudget:     tokenBudget,
		messageCount:    messageCount,
	}
}

// IsHighPressure returns true if the session is predicted to overflow soon.
// This is the "branch prediction" — we predict that the next LLM call will
// likely fail with context overflow if utilization exceeds the threshold.
func (cp *CompactionPressure) IsHighPressure(sessionKey string) bool {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	entry, ok := cp.sessions[sessionKey]
	if !ok || entry.tokenBudget == 0 {
		return false
	}
	return float64(entry.estimatedTokens)/float64(entry.tokenBudget) >= pressureThreshold
}

// Pressure returns the current utilization ratio (0.0 to 1.0+) for a session.
func (cp *CompactionPressure) Pressure(sessionKey string) float64 {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	entry, ok := cp.sessions[sessionKey]
	if !ok || entry.tokenBudget == 0 {
		return 0
	}
	return float64(entry.estimatedTokens) / float64(entry.tokenBudget)
}

// Clear removes the pressure entry for a session (e.g., after compaction).
func (cp *CompactionPressure) Clear(sessionKey string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	delete(cp.sessions, sessionKey)
}

// LogPressure logs the compaction pressure if above the threshold.
func (cp *CompactionPressure) LogPressure(sessionKey string, logger *slog.Logger) {
	cp.mu.Lock()
	entry, ok := cp.sessions[sessionKey]
	cp.mu.Unlock()
	if !ok || entry.tokenBudget == 0 {
		return
	}
	pressure := float64(entry.estimatedTokens) / float64(entry.tokenBudget)
	if pressure >= pressureThreshold {
		logger.Info("compaction pressure: high",
			"session", sessionKey,
			"pressure", pressure,
			"tokens", entry.estimatedTokens,
			"budget", entry.tokenBudget,
			"messages", entry.messageCount)
	}
}
