// dreamer.go — Dreamer interface for AuroraDream memory consolidation.
// Implemented by the memory package; autonomous owns scheduling and event emission.
package autonomous

import "context"

// Dreamer abstracts memory dreaming so autonomous doesn't import memory.
type Dreamer interface {
	// ShouldDream checks if dreaming conditions are met (turn count or time threshold).
	ShouldDream(ctx context.Context) bool
	// RunDream executes a full dreaming cycle and returns the report.
	RunDream(ctx context.Context) (*DreamReport, error)
	// IncrementTurn records a conversation turn for threshold tracking.
	IncrementTurn(ctx context.Context)
}

// DreamReport summarizes the results of a dreaming cycle.
type DreamReport struct {
	FactsVerified     int      `json:"factsVerified"`
	FactsMerged       int      `json:"factsMerged"`
	FactsExpired      int      `json:"factsExpired"`
	FactsPruned       int      `json:"factsPruned"`
	PatternsExtracted int      `json:"patternsExtracted"`
	UserModelUpdated  int      `json:"userModelUpdated"`
	MutualUpdated     int      `json:"mutualUpdated"`
	DurationMs        int64    `json:"durationMs"`
	PhaseErrors       []string `json:"phaseErrors,omitempty"`
}
