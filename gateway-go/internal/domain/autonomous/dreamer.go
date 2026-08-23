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
	// FactsVerified/FactsPruned are legacy names from the pre-WikiDreamer SQL
	// dreamer: WikiDreamer never set them and they stay 0 (wire compatibility
	// only). The measured fact-level counters are FactsMerged/FactsExpired
	// (verify fixes) and FactsLearned/FactsMoved below — all fed from the
	// per-fact audit ledger (wiki/dream_fact_ledger.go).
	FactsVerified int `json:"factsVerified"`
	FactsMerged   int `json:"factsMerged"`
	FactsExpired  int `json:"factsExpired"`
	FactsPruned   int `json:"factsPruned"`
	// FactsLearned counts synthesis proposals that actually wrote a page this
	// cycle (== len(appliedPaths) — guards-dropped proposals never count).
	FactsLearned int `json:"factsLearned,omitempty"`
	// FactsMoved counts misclassification moves auto-applied by verify.
	FactsMoved int `json:"factsMoved,omitempty"`
	// CorrectionsConsidered counts user corrections (5.7 반증 큐) the critique
	// pass saw this cycle — corrections are consumed only by an eligible cycle.
	CorrectionsConsidered int `json:"correctionsConsidered,omitempty"`
	PatternsExtracted     int `json:"patternsExtracted"`
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
	WikiProjectDigests int    `json:"wikiProjectDigests,omitempty"`
	WikiProposalPath   string `json:"wikiProposalPath,omitempty"`
	// VerifyFindings carries only FIRST-TIME advisory findings; ones already
	// announced in an earlier cycle fold into VerifyFindingsRepeat (the verify
	// ledger, wiki/verify_ledger.go, remembers what was shown).
	VerifyFindings       []string `json:"verifyFindings,omitempty"`
	VerifyFindingsRepeat int      `json:"verifyFindingsRepeat,omitempty"`
	WikiGraphNodes       int      `json:"wikiGraphNodes,omitempty"`
	WikiGraphEdges       int      `json:"wikiGraphEdges,omitempty"`
	WikiGraphClustered   bool     `json:"wikiGraphClustered,omitempty"`
	// WikiChangeSummary is a preformatted, human-readable block describing
	// what this cycle changed (paths, git snapshot hash, diffstat, rollback
	// hint). Appended verbatim to the dream notification.
	WikiChangeSummary string `json:"wikiChangeSummary,omitempty"`
	// QualityScore (0–100, 0 = not scored) grades this cycle's OUTPUT, not its
	// volume: synthesis precision (proposals surviving guards), applied-update
	// confidence, and recall utility (whether the pages the dreamer wrote in
	// earlier cycles have since been pulled into a chat turn). Advisory — a
	// rolling-window self-signal, never a CI ratchet. See dreamer_quality.go.
	QualityScore float64 `json:"qualityScore,omitempty"`
	// RecallHitPages is the count of distinct dreamer-managed pages that were
	// recalled into a chat turn inside the score window — the raw utility signal
	// behind QualityScore's utility axis.
	RecallHitPages int `json:"recallHitPages,omitempty"`
	// RecallDemandTerms are the topics of recent CUE turns the wiki could not
	// answer (wiki/recall_misses.go) — measured holes in the curated memory,
	// most-asked first. The research lane targets the same signal.
	RecallDemandTerms []string `json:"recallDemandTerms,omitempty"`
	// UnrecalledFindings counts cold pages (old + never recalled + low
	// importance) verify flagged as archive candidates this cycle. Advisory.
	UnrecalledFindings int `json:"unrecalledFindings,omitempty"`
	// CritiqueDropped counts synthesis proposals the offline self-critique pass
	// rejected before the apply stage (dreamer_critique.go). 0 when the pass is
	// disabled or found nothing to drop.
	CritiqueDropped int `json:"critiqueDropped,omitempty"`
	// StaleDuesClosed counts frontmatter dues cleared this cycle because they
	// were 7+ days past and the diary did not mention the page.
	StaleDuesClosed int `json:"staleDuesClosed,omitempty"`
	// StaleDueAlert is the top-5 closed-then-remaining overdue dues for the
	// dream notification (title + date + age).
	StaleDueAlert []string `json:"staleDueAlert,omitempty"`
	// MoreBacklog is true when the cycle consumed a capped chunk and unprocessed
	// diary/memory input remains — the autonomous service drains it with a
	// near-term re-trigger instead of waiting the full interval.
	MoreBacklog bool     `json:"moreBacklog,omitempty"`
	DurationMs  int64    `json:"durationMs"`
	PhaseErrors []string `json:"phaseErrors,omitempty"`
}
