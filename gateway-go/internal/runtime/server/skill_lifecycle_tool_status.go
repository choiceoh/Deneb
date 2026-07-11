package server

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// Skill-lifecycle status surface split out of skill_lifecycle_tool.go (pure
// move, no behavior change): per-skill/global status assembly and the Propus
// overview/doctrine maps.

// SkillLifecycleStatus returns the current skill-lifecycle status.
func (b *skillLifecycleBackend) SkillLifecycleStatus(_ context.Context, req chattools.SkillLifecycleStatusRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"system":   propusSystemStatus(strings.TrimSpace(req.SkillName)),
			"overview": propusUnavailableOverview(strings.TrimSpace(req.SkillName)),
			"ok":       false,
			"reason":   "skill tracker is not configured",
		}, nil
	}

	limit := normalizeSkillLifecycleStatusLimit(req.Limit)
	recent, err := b.tracker.RecentLifecycleLog(limit)
	if err != nil {
		return nil, err
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName != "" {
		return b.skillLifecycleStatusForSkill(skillName, limit, recent)
	}
	return b.globalSkillLifecycleStatus(limit, recent)
}

func (b *skillLifecycleBackend) skillLifecycleStatusForSkill(skillName string, limit int, recent []genesis.LifecycleLogEntry) (map[string]any, error) {
	recent = filterSkillLifecycleLog(recent, skillName)
	stats, err := b.tracker.Stats(skillName)
	if err != nil {
		return nil, err
	}
	curator, err := b.tracker.SkillCuratorReport(skillName)
	if err != nil {
		return nil, err
	}
	common := b.collectSkillLifecycleCommonStatus(skillName, limit)
	optimizerMemory, optimizerMemoryErr := b.optimizerMemory(skillName)
	status := map[string]any{
		"system":          propusSystemStatus(skillName),
		"overview":        propusSkillOverview(skillName, recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections),
		"ok":              true,
		"skillName":       skillName,
		"limit":           limit,
		"recent":          recent,
		"stats":           stats,
		"curator":         curator,
		"optimizerMemory": optimizerMemory,
	}
	common.addToStatus(status)
	if optimizerMemoryErr != "" {
		status["optimizerMemoryError"] = optimizerMemoryErr
	}
	return status, nil
}

func (b *skillLifecycleBackend) globalSkillLifecycleStatus(limit int, recent []genesis.LifecycleLogEntry) (map[string]any, error) {
	stats, err := b.tracker.ListAllStats()
	if err != nil {
		return nil, err
	}
	curator, err := b.tracker.SkillCuratorReport("")
	if err != nil {
		return nil, err
	}
	common := b.collectSkillLifecycleCommonStatus("", limit)
	selfHarnessSignals := b.tracker.SelfHarnessSignals()
	status := map[string]any{
		"system":             propusSystemStatus(""),
		"overview":           propusGlobalOverview(recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections, selfHarnessSignals),
		"ok":                 true,
		"limit":              limit,
		"recent":             recent,
		"stats":              stats,
		"curator":            curator,
		"selfHarnessSignals": selfHarnessSignals,
		// Fleet-wide failure clusters (Self-Harness weakness mining) — the same
		// evidence bundle the sweep nudge quotes, so a turn drilling in via
		// status sees the full support-ordered list, not just the top slice.
		"failureClusters": b.tracker.FailureEvidenceClusters(0),
		// Synthetic exercise lane liveness — the workout lane otherwise surfaces
		// only indirectly (workout-failure clusters), so this is its "is it
		// running" line in the integrated status view.
		"workoutActivity": b.tracker.WorkoutActivitySummarize(),
	}
	common.addToStatus(status)
	return status, nil
}

type skillLifecycleCommonStatus struct {
	rejectedEdits      []genesis.RejectedSkillEditRecord
	rejectedEditsErr   string
	usageQuality       genesis.UsageQualitySummary
	usageQualityErr    string
	validationCases    []genesis.SkillValidationCaseRecord
	validationCasesErr string
	validationSummary  genesis.SkillValidationCaseSummary
	validationErr      string
	opportunities      []genesis.SkillOpportunityRecord
	opportunitiesErr   string
	selfCorrections    []genesis.SelfCorrectionCandidateRecord
	selfCorrectionsErr string
}

func (b *skillLifecycleBackend) collectSkillLifecycleCommonStatus(skillName string, limit int) skillLifecycleCommonStatus {
	rejectedEdits, rejectedEditsErr := b.recentRejectedSkillEdits(skillName, limit)
	usageQuality, usageQualityErr := b.usageQualitySummary(skillName)
	validationCases, validationCasesErr := b.recentSkillValidationCases(skillName, limit)
	validationSummary, validationErr := b.validationCaseSummary(skillName)
	opportunities, opportunitiesErr := b.recentSkillOpportunities(skillName, limit)
	selfCorrections, selfCorrectionsErr := b.recentSelfCorrectionCandidates(skillName, limit)
	return skillLifecycleCommonStatus{
		rejectedEdits:      rejectedEdits,
		rejectedEditsErr:   rejectedEditsErr,
		usageQuality:       usageQuality,
		usageQualityErr:    usageQualityErr,
		validationCases:    validationCases,
		validationCasesErr: validationCasesErr,
		validationSummary:  validationSummary,
		validationErr:      validationErr,
		opportunities:      opportunities,
		opportunitiesErr:   opportunitiesErr,
		selfCorrections:    selfCorrections,
		selfCorrectionsErr: selfCorrectionsErr,
	}
}

func (s skillLifecycleCommonStatus) addToStatus(status map[string]any) {
	status["rejectedEdits"] = s.rejectedEdits
	status["usageQuality"] = s.usageQuality
	status["validationCases"] = s.validationCases
	status["validationCaseSummary"] = s.validationSummary
	status["opportunities"] = s.opportunities
	status["selfCorrectionCandidates"] = s.selfCorrections
	addStatusError(status, "rejectedEditsError", s.rejectedEditsErr)
	addStatusError(status, "usageQualityError", s.usageQualityErr)
	addStatusError(status, "validationCasesError", s.validationCasesErr)
	addStatusError(status, "validationCaseSummaryError", s.validationErr)
	addStatusError(status, "opportunitiesError", s.opportunitiesErr)
	addStatusError(status, "selfCorrectionCandidatesError", s.selfCorrectionsErr)
}

func addStatusError(status map[string]any, key, errText string) {
	if errText != "" {
		status[key] = errText
	}
}

func (b *skillLifecycleBackend) recentRejectedSkillEdits(skillName string, limit int) ([]genesis.RejectedSkillEditRecord, string) {
	rejected, err := b.tracker.RecentRejectedSkillEdits(skillName, limit)
	if err == nil {
		return rejected, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: rejected edits unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.RejectedSkillEditRecord{}, err.Error()
}

func (b *skillLifecycleBackend) usageQualitySummary(skillName string) (genesis.UsageQualitySummary, string) {
	quality, err := b.tracker.UsageQualitySummary(skillName)
	if err == nil {
		return quality, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: usage quality unavailable",
			"skill", skillName, "error", err)
	}
	return quality, err.Error()
}

func (b *skillLifecycleBackend) optimizerMemory(skillName string) (genesis.SkillOptimizerMemoryEntry, string) {
	memory, err := b.tracker.OptimizerMemory(skillName)
	if err == nil {
		return memory, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: optimizer memory unavailable",
			"skill", skillName, "error", err)
	}
	return memory, err.Error()
}

func (b *skillLifecycleBackend) recentSkillValidationCases(skillName string, limit int) ([]genesis.SkillValidationCaseRecord, string) {
	cases, err := b.tracker.RecentSkillValidationCases(skillName, limit)
	if err == nil {
		return cases, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: validation cases unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SkillValidationCaseRecord{}, err.Error()
}

func (b *skillLifecycleBackend) validationCaseSummary(skillName string) (genesis.SkillValidationCaseSummary, string) {
	summary, err := b.tracker.ValidationCaseSummary(skillName)
	if err == nil {
		return summary, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: validation case summary unavailable",
			"skill", skillName, "error", err)
	}
	return genesis.SkillValidationCaseSummary{}, err.Error()
}

func (b *skillLifecycleBackend) recentSkillOpportunities(skillName string, limit int) ([]genesis.SkillOpportunityRecord, string) {
	opportunities, err := b.tracker.RecentSkillOpportunities(skillName, limit)
	if err == nil {
		return opportunities, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: opportunities unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SkillOpportunityRecord{}, err.Error()
}

func (b *skillLifecycleBackend) recentSelfCorrectionCandidates(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, string) {
	candidates, err := b.tracker.RecentSelfCorrectionCandidates(skillName, genesis.SelfCorrectionStatusProposed, limit)
	if err == nil {
		return candidates, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: self-correction candidates unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SelfCorrectionCandidateRecord{}, err.Error()
}

func propusUnavailableOverview(skillName string) map[string]any {
	scope := "global"
	if strings.TrimSpace(skillName) != "" {
		scope = "skill"
	}
	return map[string]any{
		"state":       "unavailable",
		"scope":       scope,
		"skillName":   strings.TrimSpace(skillName),
		"nextActions": []string{"configure_skill_tracker"},
	}
}

func propusSkillOverview(
	skillName string,
	recent []genesis.LifecycleLogEntry,
	stats *genesis.UsageStats,
	curator []genesis.SkillCuratorRecord,
	usageQuality genesis.UsageQualitySummary,
	validationSummary genesis.SkillValidationCaseSummary,
	opportunities []genesis.SkillOpportunityRecord,
	selfCorrections []genesis.SelfCorrectionCandidateRecord,
) map[string]any {
	return propusOverviewMap(genesis.BuildPropusOverview(genesis.PropusOverviewInput{
		Scope:             genesis.PropusScopeSkill,
		SkillName:         skillName,
		Recent:            recent,
		SkillStats:        stats,
		Curator:           curator,
		UsageQuality:      usageQuality,
		ValidationSummary: validationSummary,
		Opportunities:     opportunities,
		SelfCorrections:   selfCorrections,
	}))
}

func propusGlobalOverview(
	recent []genesis.LifecycleLogEntry,
	stats []genesis.UsageStats,
	curator []genesis.SkillCuratorRecord,
	usageQuality genesis.UsageQualitySummary,
	validationSummary genesis.SkillValidationCaseSummary,
	opportunities []genesis.SkillOpportunityRecord,
	selfCorrections []genesis.SelfCorrectionCandidateRecord,
	selfHarnessSignals genesis.SelfHarnessSignalSummary,
) map[string]any {
	return propusOverviewMap(genesis.BuildPropusOverview(genesis.PropusOverviewInput{
		Scope:              genesis.PropusScopeGlobal,
		Recent:             recent,
		Stats:              stats,
		Curator:            curator,
		UsageQuality:       usageQuality,
		ValidationSummary:  validationSummary,
		Opportunities:      opportunities,
		SelfCorrections:    selfCorrections,
		SelfHarnessSignals: selfHarnessSignals,
	}))
}

func propusSystemStatus(skillName string) map[string]any {
	scope := "global"
	if strings.TrimSpace(skillName) != "" {
		scope = "skill"
	}
	identity := genesis.BuildPropusSystemIdentity(scope)
	return map[string]any{
		"name":               identity.Name,
		"codename":           identity.Codename,
		"version":            identity.Version,
		"tool":               identity.Tool,
		"scope":              identity.Scope,
		"description":        identity.Description,
		"loop":               identity.Loop,
		"sourcePapers":       identity.SourcePapers,
		"filteredSources":    identity.FilteredSources,
		"principles":         identity.Principles,
		"invariants":         identity.Invariants,
		"qualityGates":       identity.QualityGates,
		"sourcePrinciples":   identity.SourcePrinciples,
		"filteredPrinciples": identity.FilteredPrinciples,
	}
}

func propusOverviewMap(overview genesis.PropusOverview) map[string]any {
	out := map[string]any{
		"state":                  overview.State,
		"scope":                  overview.Scope,
		"eventCounts":            overview.EventCounts,
		"countedUsageRecords":    overview.CountedUsageRecords,
		"ignoredUsageRecords":    overview.IgnoredUsageRecords,
		"validationCases":        overview.ValidationCases,
		"pendingSelfCorrections": overview.PendingSelfCorrections,
		"openOpportunities":      overview.OpenOpportunities,
		"selfHarnessRejections":  overview.SelfHarnessRejections,
		"selfHarnessDrafts":      overview.SelfHarnessDrafts,
		"selfHarnessRecurrences": overview.SelfHarnessRecurrences,
		"doctrineCoverage":       propusDoctrineCoverageMap(overview.DoctrineCoverage),
		"nextActions":            overview.NextActions,
	}
	if overview.SkillName != "" {
		out["skillName"] = overview.SkillName
	}
	if overview.Scope == genesis.PropusScopeSkill {
		out["totalUses"] = overview.TotalUses
		out["successRate"] = overview.SuccessRate
		out["curatorState"] = overview.CuratorState
		out["createdBy"] = overview.CreatedBy
	} else {
		out["trackedSkills"] = overview.TrackedSkills
		out["lowSuccessSkills"] = overview.LowSuccessSkills
		out["skillsWithValidation"] = overview.SkillsWithValidation
		out["curatedSkills"] = overview.CuratedSkills
		out["staleSkills"] = overview.StaleSkills
		out["archivedSkills"] = overview.ArchivedSkills
	}
	return out
}

func propusDoctrineCoverageMap(coverage genesis.PropusDoctrineCoverage) map[string]any {
	return map[string]any{
		"state":              coverage.State,
		"covered":            coverage.Covered,
		"gaps":               coverage.Gaps,
		"axisCoverage":       coverage.AxisCoverage,
		"sourcePolicy":       coverage.SourcePolicy,
		"filteredSources":    coverage.FilteredSources,
		"selfHarnessAudits":  coverage.SelfHarnessAudits,
		"validationCases":    coverage.ValidationCases,
		"easyAnchorCases":    coverage.EasyAnchorCases,
		"mixedFrontierCases": coverage.MixedFrontierCases,
		"hardFrontierCases":  coverage.HardFrontierCases,
		"opportunities":      coverage.Opportunities,
	}
}
