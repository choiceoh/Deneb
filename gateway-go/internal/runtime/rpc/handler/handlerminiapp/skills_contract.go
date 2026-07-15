package handlerminiapp

// SkillRow is one entry in the Settings skills list. A slim projection of
// skills.SkillEntry — only the fields the native list/detail screens render.
// Behavior lives in the skillsrpc subpackage; this file stays the client
// generator's source of truth for the //deneb:wire types.
//
//deneb:wire
type SkillRow struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Category      string   `json:"category,omitempty"`
	Homepage      string   `json:"homepage,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	RelatedSkills []string `json:"relatedSkills,omitempty"`
	// Source is the discovery origin: managed | workspace |
	// agents-skills-personal | agents-skills-project | bundled | plugin | extra.
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
	// Origin separates Propus-authored output from pre-existing skills:
	// "genesis" (the loop created it) | "initial" (installed or hand-authored).
	Origin string `json:"origin,omitempty"`
	// CreatedAt is the genesis creation time (unix millis); 0 for initial skills.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// EvolveCount / LastEvolvedAt summarize committed evolve rewrites from the
	// lifecycle log — covers generated and initial skills alike.
	EvolveCount   int   `json:"evolveCount,omitempty"`
	LastEvolvedAt int64 `json:"lastEvolvedAt,omitempty"`
	// TotalUses / LastUsedAt are tracker usage aggregates.
	TotalUses  int   `json:"totalUses,omitempty"`
	LastUsedAt int64 `json:"lastUsedAt,omitempty"`
	// CuratorState is active | stale | archived for curator-managed
	// (agent-created) skills; empty for initial skills.
	CuratorState string `json:"curatorState,omitempty"`
	// Editable / Deletable are true only for local mutable skill sources.
	// Bundled and plugin skills are visible but protected from native writes.
	Editable  bool `json:"editable,omitempty"`
	Deletable bool `json:"deletable,omitempty"`
	// DependencySummary / InstallSummary expose the metadata.deneb.requires and
	// metadata.deneb.install hints that decide whether a skill is eligible and
	// how the operator can satisfy missing host dependencies.
	DependencySummary []string `json:"dependencySummary,omitempty"`
	InstallSummary    []string `json:"installSummary,omitempty"`
}

// SkillsListResponse is the miniapp.skills.list payload.
//
//deneb:wire
type SkillsListResponse struct {
	Skills []SkillRow `json:"skills"`
	Count  int        `json:"count"`
}

// SkillLifecycleEvent is one entry in the Propus timeline:
// a skill creation, a committed evolve, a rejected/rolled-back evolve, or a
// review decision (the per-session routing verdict that precedes them).
//
//deneb:wire
type SkillLifecycleEvent struct {
	// Type: genesis | evolved | evolve_rejected | evolve_rolled_back | review.
	Type      string `json:"type"`
	SkillName string `json:"skillName,omitempty"`
	At        int64  `json:"at,omitempty"` // unix millis
	// Version is the new version of a committed evolve.
	Version string `json:"version,omitempty"`
	// Detail is the human summary (description or reason). The timeline row
	// clamps it visually and reveals the full text when expanded.
	Detail string `json:"detail,omitempty"`
	// Route is the review decision for type=review: no-op | evolve | create | genesis.
	Route string `json:"route,omitempty"`
	// Evidence is the session observation a review verdict was based on —
	// only set when it isn't already serving as Detail.
	Evidence string `json:"evidence,omitempty"`
	// Self-Harness audit fields keep the target failure mechanism and
	// regression risk queryable for evolved/rejected events.
	TargetSignature        string `json:"targetSignature,omitempty"`
	EditedSurface          string `json:"editedSurface,omitempty"`
	ExpectedBehaviorChange string `json:"expectedBehaviorChange,omitempty"`
	RegressionRisk         string `json:"regressionRisk,omitempty"`
}

// PropusLifecycleSummary is the server-owned summary for the native Propus log.
// Keep this in the payload instead of recomputing it in the client: Propus has
// one state model, and the UI should render that model rather than drifting into
// a second interpretation of the same event feed.
//
//deneb:wire
type PropusLifecycleSummary struct {
	System          string   `json:"system"`
	State           string   `json:"state"`
	Total           int      `json:"total"`
	Genesis         int      `json:"genesis"`
	Evolved         int      `json:"evolved"`
	Review          int      `json:"review"`
	Rejected        int      `json:"rejected"`
	RolledBack      int      `json:"rolledBack"`
	Attention       int      `json:"attention"`
	LatestAt        int64    `json:"latestAt,omitempty"`
	LatestType      string   `json:"latestType,omitempty"`
	LatestSkill     string   `json:"latestSkill,omitempty"`
	DoctrineVersion string   `json:"doctrineVersion,omitempty"`
	Doctrine        string   `json:"doctrine,omitempty"`
	SourcePapers    []string `json:"sourcePapers,omitempty"`
	FilteredSources []string `json:"filteredSources,omitempty"`
	Principles      []string `json:"principles,omitempty"`
	QualityGates    []string `json:"qualityGates,omitempty"`
	NextActions     []string `json:"nextActions,omitempty"`
	CoverageState   string   `json:"coverageState,omitempty"`
	CoverageGaps    []string `json:"coverageGaps,omitempty"`
	NextCue         string   `json:"nextCue,omitempty"`
	QualityGate     string   `json:"qualityGate,omitempty"`
	AttentionCue    string   `json:"attentionCue,omitempty"`
}

// SkillsLifecycleResponse is the miniapp.skills.lifecycle payload,
// newest first.
//
//deneb:wire
type SkillsLifecycleResponse struct {
	Events  []SkillLifecycleEvent  `json:"events"`
	Count   int                    `json:"count"`
	Summary PropusLifecycleSummary `json:"summary"`
}

// SkillDetailResponse is the miniapp.skills.detail payload: the same enriched
// row the list renders plus the SKILL.md document itself, so the detail screen
// can show what the skill actually instructs the agent to do.
//
//deneb:wire
type SkillDetailResponse struct {
	Skill SkillRow `json:"skill"`
	// Body is the raw SKILL.md markdown (frontmatter included). Empty when the
	// file is unreadable — the detail still renders from the row meta.
	Body string `json:"body,omitempty"`
	// BodyTruncated marks a Body capped at skillBodyMaxRunes.
	BodyTruncated bool `json:"bodyTruncated,omitempty"`
	// Path is the SKILL.md location on the gateway host (operator reference).
	Path string `json:"path,omitempty"`
}
