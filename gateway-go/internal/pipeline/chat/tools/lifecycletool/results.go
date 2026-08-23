package lifecycletool

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
)

// HeartbeatShadowReplayFixtureResult is one original/candidate comparison in
// a heartbeat shadow replay.
type HeartbeatShadowReplayFixtureResult struct {
	FiredAt       int64  `json:"firedAt"`
	Split         string `json:"split"`
	Quiet         bool   `json:"quiet"`
	OriginalPass  bool   `json:"originalPass"`
	CandidatePass bool   `json:"candidatePass"`
	Note          string `json:"note,omitempty"`
}

// HeartbeatShadowReplayResult reports a dry-run heartbeat contract comparison.
type HeartbeatShadowReplayResult struct {
	OK                bool                                 `json:"ok"`
	Verdict           string                               `json:"verdict"`
	Reason            string                               `json:"reason"`
	Fixtures          int                                  `json:"fixtures"`
	HeldInOriginal    int                                  `json:"heldInOriginal"`
	HeldInCandidate   int                                  `json:"heldInCandidate"`
	HeldInTotal       int                                  `json:"heldInTotal"`
	HeldOutOriginal   int                                  `json:"heldOutOriginal"`
	HeldOutCandidate  int                                  `json:"heldOutCandidate"`
	HeldOutTotal      int                                  `json:"heldOutTotal"`
	Results           []HeartbeatShadowReplayFixtureResult `json:"results,omitempty"`
	DryRun            bool                                 `json:"dryRun"`
	ContractDriftNote string                               `json:"contractDriftNote,omitempty"`
}

// SkillEvolutionProposalResult records a routing decision and, when requested,
// the concrete execution result for the selected route.
type SkillEvolutionProposalResult struct {
	OK         bool   `json:"ok"`
	Candidate  string `json:"candidate"`
	Route      string `json:"route"`
	Executed   bool   `json:"executed"`
	Reason     string `json:"reason,omitempty"`
	NextAction string `json:"nextAction,omitempty"`
	// Suppressed carries the evolver's gate reason when a route=evolve proposal
	// named a skill the deterministic gates refuse right now (thrash cooldown,
	// rejection backoff, recency gate). The proposal is recorded, not executed.
	Suppressed string                                 `json:"suppressed,omitempty"`
	Error      string                                 `json:"error,omitempty"`
	Result     *SkillEvolutionProposalExecutionResult `json:"result,omitempty"`
}

// SkillEvolutionProposalExecutionResult is a closed union of the routes that
// the propose action can execute directly. It marshals as the selected result,
// preserving the existing tool JSON without exposing an any-typed boundary.
type SkillEvolutionProposalExecutionResult struct {
	Genesis   *SkillGenesisResult
	Evolution *SkillEvolutionResult
}

// MarshalJSON renders the selected proposal execution result without a union
// wrapper in the agent-facing payload.
func (r SkillEvolutionProposalExecutionResult) MarshalJSON() ([]byte, error) {
	switch {
	case r.Genesis != nil && r.Evolution == nil:
		return json.Marshal(r.Genesis)
	case r.Genesis == nil && r.Evolution != nil:
		return json.Marshal(r.Evolution)
	default:
		return nil, fmt.Errorf("skill lifecycle proposal result must contain exactly one execution result")
	}
}

// SkillGenesisResult describes a generated skill or an intentional skip.
type SkillGenesisResult struct {
	OK     bool                       `json:"ok"`
	Skip   bool                       `json:"skip,omitempty"`
	Reason string                     `json:"reason,omitempty"`
	Source string                     `json:"source"`
	Skill  *generation.GeneratedSkill `json:"skill,omitempty"`
}

// SkillEvolutionResult describes one evolution attempt.
type SkillEvolutionResult struct {
	OK     bool                  `json:"ok"`
	Result *genesis.EvolveResult `json:"result"`
}

// SkillCuratorActionResult describes a manual curator transition. Pointer
// fields distinguish a successful empty report from an unconfigured backend.
type SkillCuratorActionResult struct {
	OK        bool                `json:"ok"`
	Reason    string              `json:"reason,omitempty"`
	Action    *string             `json:"action,omitempty"`
	SkillName *string             `json:"skillName,omitempty"`
	Curator   *SkillCuratorResult `json:"curator,omitempty"`
}

// SkillCuratorResult preserves the established curator contract: state
// transitions return one record, while pin toggles return the queried slice.
type SkillCuratorResult struct {
	Record  *genesis.SkillCuratorRecord
	Records *[]genesis.SkillCuratorRecord
}

// MarshalJSON renders the selected curator result without a union wrapper.
func (r SkillCuratorResult) MarshalJSON() ([]byte, error) {
	switch {
	case r.Record != nil && r.Records == nil:
		return json.Marshal(r.Record)
	case r.Record == nil && r.Records != nil:
		return json.Marshal(r.Records)
	default:
		return nil, fmt.Errorf("skill curator result must contain exactly one record shape")
	}
}

// SkillValidationCaseResult describes a manually recorded validation case.
type SkillValidationCaseResult struct {
	OK        bool    `json:"ok"`
	Reason    string  `json:"reason,omitempty"`
	SkillName *string `json:"skillName,omitempty"`
	ID        *string `json:"id,omitempty"`
}

// SkillValidationCaseFromSessionResult describes a validation case extracted
// from one transcript, including intentional weak-evidence skips.
type SkillValidationCaseFromSessionResult struct {
	OK                 bool    `json:"ok"`
	Skip               bool    `json:"skip,omitempty"`
	Reason             string  `json:"reason,omitempty"`
	SkillName          *string `json:"skillName,omitempty"`
	ID                 *string `json:"id,omitempty"`
	SessionKey         *string `json:"sessionKey,omitempty"`
	ExpectedToolCalls  *int    `json:"expectedToolCalls,omitempty"`
	ForbiddenToolCalls *int    `json:"forbiddenToolCalls,omitempty"`
	RequiredTools      *int    `json:"requiredTools,omitempty"`
}

// SkillValidationBackfillDetail is one session outcome in a validation-case
// backfill report.
type SkillValidationBackfillDetail struct {
	SessionKey         string  `json:"sessionKey"`
	OK                 bool    `json:"ok"`
	Skip               bool    `json:"skip,omitempty"`
	Reason             string  `json:"reason,omitempty"`
	Error              string  `json:"error,omitempty"`
	ID                 *string `json:"id,omitempty"`
	ExpectedToolCalls  *int    `json:"expectedToolCalls,omitempty"`
	ForbiddenToolCalls *int    `json:"forbiddenToolCalls,omitempty"`
	RequiredTools      *int    `json:"requiredTools,omitempty"`
}

// SkillValidationBackfillResult summarizes transcript scanning and the cases
// that were recorded, skipped, or rejected.
type SkillValidationBackfillResult struct {
	OK                         bool                                `json:"ok"`
	Reason                     string                              `json:"reason,omitempty"`
	SkillName                  *string                             `json:"skillName,omitempty"`
	Limit                      *int                                `json:"limit,omitempty"`
	Scanned                    *int                                `json:"scanned,omitempty"`
	Recorded                   *int                                `json:"recorded,omitempty"`
	Skipped                    *int                                `json:"skipped,omitempty"`
	Errors                     *[]string                           `json:"errors,omitempty"`
	Details                    *[]SkillValidationBackfillDetail    `json:"details,omitempty"`
	ValidationCaseSummary      *genesis.SkillValidationCaseSummary `json:"validationCaseSummary,omitempty"`
	ValidationCaseSummaryError string                              `json:"validationCaseSummaryError,omitempty"`
}

// SkillSelfCorrectionCandidateResult describes a queued correction candidate.
type SkillSelfCorrectionCandidateResult struct {
	OK        bool                                   `json:"ok"`
	Reason    string                                 `json:"reason,omitempty"`
	Candidate *genesis.SelfCorrectionCandidateRecord `json:"candidate,omitempty"`
}

// SkillSelfCorrectionReviewResult describes the append-only review record for
// a correction candidate.
type SkillSelfCorrectionReviewResult struct {
	OK     bool                                   `json:"ok"`
	Reason string                                 `json:"reason,omitempty"`
	Review *genesis.SelfCorrectionCandidateRecord `json:"review,omitempty"`
}

// SkillLifecycleStats is the status endpoint's closed skill/global stats
// union. Scope selects whether JSON contains one object or the fleet slice.
type SkillLifecycleStats struct {
	Scope string `json:"-"`
	Skill *genesis.UsageStats
	Fleet []genesis.UsageStats
}

// MarshalJSON preserves the pre-existing object-vs-array status contract.
func (s SkillLifecycleStats) MarshalJSON() ([]byte, error) {
	switch s.Scope {
	case propus.PropusScopeSkill:
		return json.Marshal(s.Skill)
	case propus.PropusScopeGlobal:
		return json.Marshal(s.Fleet)
	default:
		return nil, fmt.Errorf("unsupported skill lifecycle stats scope %q", s.Scope)
	}
}

// SkillLifecycleUnavailableOverview is emitted when the tracker dependency is
// absent. Its deliberately small shape matches the legacy status payload.
type SkillLifecycleUnavailableOverview struct {
	State       string   `json:"state"`
	Scope       string   `json:"scope"`
	SkillName   string   `json:"skillName"`
	NextActions []string `json:"nextActions"`
}

// SkillLifecycleOverview wraps the Propus operational state or its unavailable
// variant while retaining a concrete boundary type.
type SkillLifecycleOverview struct {
	Operational *propus.PropusOverview
	Unavailable *SkillLifecycleUnavailableOverview
}

// MarshalJSON keeps the historical status shape, including zero-valued fields
// that are meaningful for the selected skill/global scope.
func (o SkillLifecycleOverview) MarshalJSON() ([]byte, error) {
	if o.Unavailable != nil {
		if o.Operational != nil {
			return nil, fmt.Errorf("skill lifecycle overview cannot be both operational and unavailable")
		}
		return json.Marshal(o.Unavailable)
	}
	if o.Operational == nil {
		return nil, fmt.Errorf("skill lifecycle overview is empty")
	}
	base := skillLifecycleOverviewBaseFrom(*o.Operational)
	if o.Operational.Scope == propus.PropusScopeSkill {
		return json.Marshal(skillLifecycleSkillOverview{
			skillLifecycleOverviewBase: base,
			SkillName:                  o.Operational.SkillName,
			TotalUses:                  o.Operational.TotalUses,
			SuccessRate:                o.Operational.SuccessRate,
			CuratorState:               o.Operational.CuratorState,
			CreatedBy:                  o.Operational.CreatedBy,
		})
	}
	return json.Marshal(skillLifecycleGlobalOverview{
		skillLifecycleOverviewBase: base,
		TrackedSkills:              o.Operational.TrackedSkills,
		LowSuccessSkills:           o.Operational.LowSuccessSkills,
		SkillsWithValidation:       o.Operational.SkillsWithValidation,
		CuratedSkills:              o.Operational.CuratedSkills,
		StaleSkills:                o.Operational.StaleSkills,
		ArchivedSkills:             o.Operational.ArchivedSkills,
	})
}

type skillLifecycleOverviewBase struct {
	State                  string                        `json:"state"`
	Scope                  string                        `json:"scope"`
	EventCounts            map[string]int                `json:"eventCounts"`
	CountedUsageRecords    int                           `json:"countedUsageRecords"`
	IgnoredUsageRecords    int                           `json:"ignoredUsageRecords"`
	ValidationCases        int                           `json:"validationCases"`
	PendingSelfCorrections int                           `json:"pendingSelfCorrections"`
	OpenOpportunities      int                           `json:"openOpportunities"`
	SelfHarnessRejections  int                           `json:"selfHarnessRejections"`
	SelfHarnessDrafts      int                           `json:"selfHarnessDrafts"`
	SelfHarnessRecurrences int                           `json:"selfHarnessRecurrences"`
	DoctrineCoverage       propus.PropusDoctrineCoverage `json:"doctrineCoverage"`
	NextActions            []string                      `json:"nextActions"`
}

type skillLifecycleSkillOverview struct {
	skillLifecycleOverviewBase
	SkillName    string  `json:"skillName,omitempty"`
	TotalUses    int     `json:"totalUses"`
	SuccessRate  float64 `json:"successRate"`
	CuratorState string  `json:"curatorState"`
	CreatedBy    string  `json:"createdBy"`
}

type skillLifecycleGlobalOverview struct {
	skillLifecycleOverviewBase
	TrackedSkills        int `json:"trackedSkills"`
	LowSuccessSkills     int `json:"lowSuccessSkills"`
	SkillsWithValidation int `json:"skillsWithValidation"`
	CuratedSkills        int `json:"curatedSkills"`
	StaleSkills          int `json:"staleSkills"`
	ArchivedSkills       int `json:"archivedSkills"`
}

func skillLifecycleOverviewBaseFrom(overview propus.PropusOverview) skillLifecycleOverviewBase {
	return skillLifecycleOverviewBase{
		State:                  overview.State,
		Scope:                  overview.Scope,
		EventCounts:            overview.EventCounts,
		CountedUsageRecords:    overview.CountedUsageRecords,
		IgnoredUsageRecords:    overview.IgnoredUsageRecords,
		ValidationCases:        overview.ValidationCases,
		PendingSelfCorrections: overview.PendingSelfCorrections,
		OpenOpportunities:      overview.OpenOpportunities,
		SelfHarnessRejections:  overview.SelfHarnessRejections,
		SelfHarnessDrafts:      overview.SelfHarnessDrafts,
		SelfHarnessRecurrences: overview.SelfHarnessRecurrences,
		DoctrineCoverage:       overview.DoctrineCoverage,
		NextActions:            overview.NextActions,
	}
}

// SkillLifecycleStatusResult is the typed status contract shared by the
// runtime backend and chat adapter. Pointer fields preserve the distinction
// between an unavailable subsystem and an operational empty collection.
type SkillLifecycleStatusResult struct {
	System    propus.PropusSystemIdentity   `json:"system"`
	Overview  SkillLifecycleOverview        `json:"overview"`
	OK        bool                          `json:"ok"`
	Reason    string                        `json:"reason,omitempty"`
	SkillName string                        `json:"skillName,omitempty"`
	Limit     *int                          `json:"limit,omitempty"`
	Recent    *[]genesis.LifecycleLogEntry  `json:"recent,omitempty"`
	Stats     *SkillLifecycleStats          `json:"stats,omitempty"`
	Curator   *[]genesis.SkillCuratorRecord `json:"curator,omitempty"`

	OptimizerMemory      *genesis.SkillOptimizerMemoryEntry `json:"optimizerMemory,omitempty"`
	OptimizerMemoryError string                             `json:"optimizerMemoryError,omitempty"`

	RejectedEdits                 *[]genesis.RejectedSkillEditRecord       `json:"rejectedEdits,omitempty"`
	RejectedEditsError            string                                   `json:"rejectedEditsError,omitempty"`
	UsageQuality                  *genesis.UsageQualitySummary             `json:"usageQuality,omitempty"`
	UsageQualityError             string                                   `json:"usageQualityError,omitempty"`
	ValidationCases               *[]genesis.SkillValidationCaseRecord     `json:"validationCases,omitempty"`
	ValidationCasesError          string                                   `json:"validationCasesError,omitempty"`
	ValidationCaseSummary         *genesis.SkillValidationCaseSummary      `json:"validationCaseSummary,omitempty"`
	ValidationCaseSummaryError    string                                   `json:"validationCaseSummaryError,omitempty"`
	AblationSummary               *genesis.SkillAblationSummary            `json:"ablationSummary,omitempty"`
	AblationSummaryError          string                                   `json:"ablationSummaryError,omitempty"`
	Opportunities                 *[]genesis.SkillOpportunityRecord        `json:"opportunities,omitempty"`
	OpportunitiesError            string                                   `json:"opportunitiesError,omitempty"`
	SelfCorrectionCandidates      *[]genesis.SelfCorrectionCandidateRecord `json:"selfCorrectionCandidates,omitempty"`
	SelfCorrectionCandidatesError string                                   `json:"selfCorrectionCandidatesError,omitempty"`

	SelfHarnessSignals *genesis.SelfHarnessSignalSummary `json:"selfHarnessSignals,omitempty"`
	FailureClusters    *[]genesis.FailureClusterSummary  `json:"failureClusters,omitempty"`
	WorkoutActivity    *genesis.WorkoutActivitySummary   `json:"workoutActivity,omitempty"`
}
