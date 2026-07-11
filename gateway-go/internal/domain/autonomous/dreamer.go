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
	FactsVerified     int `json:"factsVerified"`
	FactsMerged       int `json:"factsMerged"`
	FactsExpired      int `json:"factsExpired"`
	FactsPruned       int `json:"factsPruned"`
	PatternsExtracted int `json:"patternsExtracted"`
	// UserModelUpdated counts 사용자-category wiki pages the cycle created or
	// updated — the agent's model of its user (preferences, working style,
	// personal context). Subset of WikiPagesCreated/Updated.
	UserModelUpdated    int `json:"userModelUpdated"`
	MutualUpdated       int `json:"mutualUpdated"`
	WikiPagesCreated    int `json:"wikiPagesCreated,omitempty"`
	WikiPagesUpdated    int `json:"wikiPagesUpdated,omitempty"`
	WikiUpdatesProposed int `json:"wikiUpdatesProposed,omitempty"`
	// WikiProjectDigests counts project 대표페이지 "## 현재 상태" sections the
	// cycle refreshed (Phase 3d) — page mutations outside the created/updated
	// apply counters, so digest-only cycles still register as page-changing.
	WikiProjectDigests int      `json:"wikiProjectDigests,omitempty"`
	WikiProposalPath   string   `json:"wikiProposalPath,omitempty"`
	VerifyFindings     []string `json:"verifyFindings,omitempty"`
	WikiGraphNodes     int      `json:"wikiGraphNodes,omitempty"`
	WikiGraphEdges     int      `json:"wikiGraphEdges,omitempty"`
	WikiGraphClustered bool     `json:"wikiGraphClustered,omitempty"`
	// WikiChangeSummary is a preformatted, human-readable block describing
	// what this cycle changed (paths, git snapshot hash, diffstat, rollback
	// hint). Appended verbatim to the dream notification.
	WikiChangeSummary string   `json:"wikiChangeSummary,omitempty"`
	DurationMs        int64    `json:"durationMs"`
	PhaseErrors       []string `json:"phaseErrors,omitempty"`
}
