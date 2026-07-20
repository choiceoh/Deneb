// Package memory defines governed write targets for longitudinal personal
// memory (HealthClaw-style selective change). It is a leaf: classification and
// routing only — no chat/wiki imports.
package memory

// WriteTarget is the persistence role of one induced memory item.
// Different targets must not share update or disclosure policy.
type WriteTarget string

const (
	// TargetProfile — durable personal facts (preferences, constraints, goals).
	// Routes to MEMORY.md (main session, subject=self only).
	TargetProfile WriteTarget = "profile"
	// TargetProcedure — reusable task patterns. Propose-only ledger; never
	// auto-edits skills (RSI surfaces stay gated).
	TargetProcedure WriteTarget = "procedure"
	// TargetEpisodic — single-turn context. Diary already captures this; induction
	// records a no-op route so callers do not also promote it.
	TargetEpisodic WriteTarget = "episodic"
	// TargetExclude — sensitive or inappropriate for long-term retention.
	TargetExclude WriteTarget = "exclude"
	// TargetGovernance — shared rules (SOUL/AGENTS). Never written from a user
	// episode; classifier may emit it only so the router can refuse.
	TargetGovernance WriteTarget = "governance"
)

// Valid reports whether t is a known write target.
func (t WriteTarget) Valid() bool {
	switch t {
	case TargetProfile, TargetProcedure, TargetEpisodic, TargetExclude, TargetGovernance:
		return true
	default:
		return false
	}
}

// Route is where an induced item should land after classification.
type Route string

const (
	RouteMemory      Route = "memory"     // MEMORY.md append
	RouteLedger      Route = "ledger"     // procedure/other-subject propose ledger
	RouteDiaryOnly   Route = "diary_only" // already handled by diary; skip promote
	RouteDrop        Route = "drop"       // exclude / governance refuse
	RouteUnspecified Route = ""
)

// RouteFor returns the sink for a classified target under the given subject.
// Profile facts about non-self subjects never enter MEMORY.md.
func RouteFor(target WriteTarget, subjectID string) Route {
	switch target {
	case TargetProfile:
		if NormalizeSubject(subjectID) == SubjectSelf {
			return RouteMemory
		}
		return RouteLedger
	case TargetProcedure:
		return RouteLedger
	case TargetEpisodic:
		return RouteDiaryOnly
	case TargetExclude, TargetGovernance:
		return RouteDrop
	default:
		return RouteUnspecified
	}
}
