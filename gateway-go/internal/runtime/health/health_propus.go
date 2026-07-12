package health

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/propusview"
)

type propusValues map[string]any

// PropusSection is an immutable health snapshot. Its wire representation stays
// private to this adapter so callers cannot inject arbitrary fields into the
// canonical Propus health contract.
type PropusSection struct {
	values propusValues
}

// MarshalJSON renders the established flat /health payload.
func (s PropusSection) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.values)
}

// Propus snapshots the self-improvement control plane. The bool
// distinguishes an unwired tracker from an initialized tracker whose counters
// are all zero, preserving the optional-section behavior of /health.
func Propus(tracker *genesis.Tracker) (*PropusSection, bool) {
	if tracker == nil {
		return nil, false
	}

	liveness := tracker.LivenessSnapshot()
	identity := propus.BuildPropusSystemIdentity(propus.PropusScopeGlobal)
	lastActivity := propus.PropusLastActivityMS(propusview.Liveness(liveness))
	section := propusValues{
		"system":                identity.Name,
		"tool":                  identity.Tool,
		"doctrine_version":      identity.Version,
		"source_papers":         identity.SourcePapers,
		"filtered_sources":      identity.FilteredSources,
		"last_review_ms":        liveness.LastReviewAt,
		"last_review_ok":        liveness.LastReviewOK,
		"last_evolve_ms":        liveness.LastEvolveAt,
		"last_genesis_ms":       liveness.LastGenesisAt,
		"review_attempts":       liveness.ReviewAttempts,
		"review_skips":          liveness.ReviewSkips,
		"validation_rejections": liveness.ValidationRejections,
		"quality_gates":         identity.QualityGates,
	}
	if lastActivity > 0 {
		section["last_activity_ms"] = lastActivity
		section["last_activity_age"] = formatDuration(time.Since(time.UnixMilli(lastActivity)))
	}
	if liveness.LastReviewAt > 0 {
		section["review_age"] = formatDuration(time.Since(time.UnixMilli(liveness.LastReviewAt)))
	}
	if liveness.LastError != "" {
		section["last_error"] = liveness.LastError
	}

	// Productivity/thrash signals make a loop that repeatedly evolves one skill
	// visible here instead of only in logs.
	evolution := tracker.EvolutionHealth()
	section["evolves_7d"] = evolution.Evolves7d
	section["evolve_rejected_7d"] = evolution.EvolveRejected7d
	section["evolve_rolled_back_7d"] = evolution.EvolveRolledBack7d
	section["genesis_7d"] = evolution.Genesis7d
	section["distinct_skills_evolved_7d"] = evolution.DistinctSkillsEvolved7d
	if evolution.TopEvolvedSkill != "" {
		section["top_evolved_skill"] = evolution.TopEvolvedSkill
		section["top_evolved_count"] = evolution.TopEvolvedCount
	}
	if evolution.LastRejectedSkill != "" {
		section["last_rejected_skill"] = evolution.LastRejectedSkill
		section["last_rejected_reason"] = evolution.LastRejectedReason
	}
	if evolution.Thrash {
		section["thrash"] = true
	}

	// RSI P2 slow loop: the weekly meta-evolution cycle and its propose-only
	// output, so a silent slow loop (or a waiting .proposed) is visible here.
	if tracker.AutoAdoptFrozen() {
		section["auto_adopt_frozen"] = true
	}
	meta := tracker.MetaEvolutionHealth()
	section["meta_revisions_7d"] = meta.Revisions7d
	section["meta_proposed_7d"] = meta.Proposed7d
	if meta.LastArtifact != "" {
		section["meta_last_artifact"] = meta.LastArtifact
		section["meta_last_epoch"] = meta.LastEpoch
		section["meta_last_proposed"] = meta.LastProposed
		section["meta_last_reason"] = meta.LastReason
	}

	selfHarness := tracker.SelfHarnessSignals()
	section["self_harness_rejections_7d"] = selfHarness.Rejections7d
	section["self_harness_missing_audit_rejections_7d"] = selfHarness.MissingAuditRejections7d
	section["self_harness_signature_mismatch_rejections_7d"] = selfHarness.SignatureMismatchRejections7d
	section["self_harness_surface_mismatch_rejections_7d"] = selfHarness.SurfaceMismatchRejections7d
	section["self_harness_held_out_replay_rejections_7d"] = selfHarness.HeldOutReplayRejections7d
	section["self_harness_validation_drafts_7d"] = selfHarness.ValidationDrafts7d
	section["self_harness_target_recurrences_7d"] = selfHarness.TargetRecurrences7d
	if selfHarness.TopRecurringTargetSkill != "" {
		section["self_harness_top_recurring_target_skill"] = selfHarness.TopRecurringTargetSkill
		section["self_harness_top_recurring_target_signature"] = selfHarness.TopRecurringTargetSignature
		section["self_harness_top_recurring_target_recurrences"] = selfHarness.TopRecurringTargetRecurrences
	}

	usageQuality := collectPropusUsageQuality(tracker, section)
	validationSummary := collectPropusValidationSummary(tracker, section)
	agentSkills, unusedAgentSkills := collectPropusAgentSkillValue(tracker, section)

	recent, _ := tracker.RecentLifecycleLog(60)
	stats, _ := tracker.ListAllStats()
	curator, _ := tracker.SkillCuratorReport("")
	opportunities, _ := tracker.RecentSkillOpportunities("", 60)
	selfCorrections, _ := tracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusProposed, 60)
	overview := propus.BuildPropusOverview(propus.PropusOverviewInput{
		Scope:              propus.PropusScopeGlobal,
		Recent:             propusview.LifecycleEntries(recent),
		Stats:              propusview.UsageStats(stats),
		Curator:            propusview.Curator(curator),
		UsageQuality:       propusview.UsageQuality(usageQuality),
		ValidationSummary:  propusview.Validation(validationSummary),
		Opportunities:      propusview.Opportunities(opportunities),
		SelfCorrections:    propusview.SelfCorrections(selfCorrections),
		SelfHarnessSignals: propusview.SelfHarness(selfHarness),
	})
	health := propus.BuildPropusHealth(propus.PropusHealthInput{
		Liveness:          propusview.Liveness(liveness),
		Evolution:         propusview.Evolution(evolution),
		Validation:        propusview.Validation(validationSummary),
		SelfHarness:       propusview.SelfHarness(selfHarness),
		AgentSkills:       agentSkills,
		UnusedAgentSkills: unusedAgentSkills,
	})
	section["state"] = health.State
	section["overview_state"] = overview.State
	section["coverage_state"] = overview.DoctrineCoverage.State
	section["coverage_gaps"] = overview.DoctrineCoverage.Gaps
	section["next_actions"] = overview.NextActions
	if len(health.Attention) > 0 {
		section["attention"] = health.Attention
	}

	return &PropusSection{values: section}, true
}

func collectPropusUsageQuality(tracker *genesis.Tracker, section propusValues) genesis.UsageQualitySummary {
	quality, err := tracker.UsageQualitySummary("")
	if err != nil {
		return genesis.UsageQualitySummary{}
	}
	section["usage_records"] = quality.TotalRecords
	section["usage_counted_records"] = quality.CountedRecords
	section["ignored_usage_records"] = quality.IgnoredRecords
	if quality.IgnoredUnactionableLegacyFailures > 0 {
		section["ignored_unactionable_legacy_failures"] = quality.IgnoredUnactionableLegacyFailures
		section["top_ignored_unactionable_legacy_failure_skill"] = quality.TopIgnoredUnactionableLegacyFailureSkill
		section["top_ignored_unactionable_legacy_failure_skill_count"] = quality.TopIgnoredUnactionableLegacyFailureSkillCount
	}
	return quality
}

func collectPropusValidationSummary(tracker *genesis.Tracker, section propusValues) genesis.SkillValidationCaseSummary {
	summary, err := tracker.ValidationCaseSummary("")
	if err != nil {
		return genesis.SkillValidationCaseSummary{}
	}
	section["validation_case_records"] = summary.RawRecords
	section["validation_cases_unique"] = summary.UniqueRecords
	section["validation_case_duplicates"] = summary.DuplicateRecords
	section["validation_cases_automatic"] = summary.AutomaticRecords
	section["validation_cases_unique_automatic"] = summary.UniqueAutomaticRecords
	section["validation_cases_easy_anchor"] = summary.UniqueEasyAnchorCases
	section["validation_cases_mixed_frontier"] = summary.UniqueMixedFrontierCases
	section["validation_cases_hard_frontier"] = summary.UniqueHardFrontierCases
	section["validation_case_skills"] = summary.SkillsWithCases
	if summary.WeakAutomaticRecords > 0 {
		section["validation_cases_weak_automatic"] = summary.WeakAutomaticRecords
		section["validation_cases_unique_weak_automatic"] = summary.UniqueWeakAutomaticCases
	}
	if summary.TopSkill != "" {
		section["validation_case_top_skill"] = summary.TopSkill
		section["validation_case_top_skill_cases"] = summary.TopSkillUniqueCases
	}
	return summary
}

func collectPropusAgentSkillValue(tracker *genesis.Tracker, section propusValues) (total int, unused int) {
	trackedTotal, trackedUnused := tracker.AgentSkillValueSummary()
	if trackedTotal > 0 {
		section["agent_skills"] = trackedTotal
		section["unused_agent_skills"] = trackedUnused
		return trackedTotal, trackedUnused
	}
	return 0, 0
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remaining)
}
