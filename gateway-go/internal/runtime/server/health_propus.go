package server

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// collectPropusHealth snapshots the self-improvement control plane. The bool
// distinguishes an unwired tracker from an initialized tracker whose counters
// are all zero, preserving the optional-section behavior of /health.
func (s *Server) collectPropusHealth() (map[string]any, bool) {
	if s.genesisTracker == nil {
		return nil, false
	}

	liveness := s.genesisTracker.LivenessSnapshot()
	identity := genesis.BuildPropusSystemIdentity(genesis.PropusScopeGlobal)
	lastActivity := genesis.PropusLastActivityMS(liveness)
	section := map[string]any{
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
		section["last_activity_age"] = formatUptimeHTTP(time.Since(time.UnixMilli(lastActivity)))
	}
	if liveness.LastReviewAt > 0 {
		section["review_age"] = formatUptimeHTTP(time.Since(time.UnixMilli(liveness.LastReviewAt)))
	}
	if liveness.LastError != "" {
		section["last_error"] = liveness.LastError
	}

	// Productivity/thrash signals make a loop that repeatedly evolves one skill
	// visible here instead of only in logs.
	evolution := s.genesisTracker.EvolutionHealth()
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

	selfHarness := s.genesisTracker.SelfHarnessSignals()
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

	usageQuality := s.collectPropusUsageQuality(section)
	validationSummary := s.collectPropusValidationSummary(section)
	agentSkills, unusedAgentSkills := s.collectPropusAgentSkillValue(section)

	recent, _ := s.genesisTracker.RecentLifecycleLog(60)
	stats, _ := s.genesisTracker.ListAllStats()
	curator, _ := s.genesisTracker.SkillCuratorReport("")
	opportunities, _ := s.genesisTracker.RecentSkillOpportunities("", 60)
	selfCorrections, _ := s.genesisTracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusProposed, 60)
	overview := genesis.BuildPropusOverview(genesis.PropusOverviewInput{
		Scope:              genesis.PropusScopeGlobal,
		Recent:             recent,
		Stats:              stats,
		Curator:            curator,
		UsageQuality:       usageQuality,
		ValidationSummary:  validationSummary,
		Opportunities:      opportunities,
		SelfCorrections:    selfCorrections,
		SelfHarnessSignals: selfHarness,
	})
	health := genesis.BuildPropusHealth(genesis.PropusHealthInput{
		Liveness:          liveness,
		Evolution:         evolution,
		Validation:        validationSummary,
		SelfHarness:       selfHarness,
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

	return section, true
}

func (s *Server) collectPropusUsageQuality(section map[string]any) genesis.UsageQualitySummary {
	quality, err := s.genesisTracker.UsageQualitySummary("")
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

func (s *Server) collectPropusValidationSummary(section map[string]any) genesis.SkillValidationCaseSummary {
	summary, err := s.genesisTracker.ValidationCaseSummary("")
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

func (s *Server) collectPropusAgentSkillValue(section map[string]any) (total int, unused int) {
	trackedTotal, trackedUnused := s.genesisTracker.AgentSkillValueSummary()
	if trackedTotal > 0 {
		section["agent_skills"] = trackedTotal
		section["unused_agent_skills"] = trackedUnused
		return trackedTotal, trackedUnused
	}
	return 0, 0
}

// attachPropusHealth publishes the canonical Propus name and the legacy
// self_evolution compatibility name as aliases of the same snapshot.
func attachPropusHealth(health map[string]any, section map[string]any) {
	health["propus"] = section
	health["self_evolution"] = section
}
