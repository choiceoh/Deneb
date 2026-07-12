package skilllifecycle

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/propusview"
)

// Skill-lifecycle status surface split out of skill_lifecycle_tool.go (pure
// move, no behavior change): per-skill/global status assembly and the Propus
// overview/doctrine DTOs.

// SkillLifecycleStatus returns the current skill-lifecycle status.
func (b *skillLifecycleBackend) SkillLifecycleStatus(_ context.Context, req chattools.SkillLifecycleStatusRequest) (chattools.SkillLifecycleStatusResult, error) {
	if b.tracker == nil {
		return chattools.SkillLifecycleStatusResult{
			System:   propusSystemStatus(strings.TrimSpace(req.SkillName)),
			Overview: propusUnavailableOverview(strings.TrimSpace(req.SkillName)),
			Reason:   "skill tracker is not configured",
		}, nil
	}

	limit := normalizeSkillLifecycleStatusLimit(req.Limit)
	recent, err := b.tracker.RecentLifecycleLog(limit)
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName != "" {
		return b.skillLifecycleStatusForSkill(skillName, limit, recent)
	}
	return b.globalSkillLifecycleStatus(limit, recent)
}

func (b *skillLifecycleBackend) skillLifecycleStatusForSkill(skillName string, limit int, recent []genesis.LifecycleLogEntry) (chattools.SkillLifecycleStatusResult, error) {
	recent = filterSkillLifecycleLog(recent, skillName)
	stats, err := b.tracker.Stats(skillName)
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	curator, err := b.tracker.SkillCuratorReport(skillName)
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	common := b.collectSkillLifecycleCommonStatus(skillName, limit)
	optimizerMemory, optimizerMemoryErr := b.optimizerMemory(skillName)
	status := chattools.SkillLifecycleStatusResult{
		System:          propusSystemStatus(skillName),
		Overview:        propusSkillOverview(skillName, recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections),
		OK:              true,
		SkillName:       skillName,
		Limit:           lifecycleValue(limit),
		Recent:          lifecycleValue(recent),
		Stats:           &chattools.SkillLifecycleStats{Scope: propus.PropusScopeSkill, Skill: stats},
		Curator:         lifecycleValue(curator),
		OptimizerMemory: lifecycleValue(optimizerMemory),
	}
	common.addToStatus(&status)
	status.OptimizerMemoryError = optimizerMemoryErr
	return status, nil
}

func (b *skillLifecycleBackend) globalSkillLifecycleStatus(limit int, recent []genesis.LifecycleLogEntry) (chattools.SkillLifecycleStatusResult, error) {
	stats, err := b.tracker.ListAllStats()
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	curator, err := b.tracker.SkillCuratorReport("")
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	common := b.collectSkillLifecycleCommonStatus("", limit)
	selfHarnessSignals := b.tracker.SelfHarnessSignals()
	failureClusters := b.tracker.FailureEvidenceClusters(0)
	workoutActivity := b.tracker.WorkoutActivitySummarize()
	status := chattools.SkillLifecycleStatusResult{
		System:             propusSystemStatus(""),
		Overview:           propusGlobalOverview(recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections, selfHarnessSignals),
		OK:                 true,
		Limit:              lifecycleValue(limit),
		Recent:             lifecycleValue(recent),
		Stats:              &chattools.SkillLifecycleStats{Scope: propus.PropusScopeGlobal, Fleet: stats},
		Curator:            lifecycleValue(curator),
		SelfHarnessSignals: lifecycleValue(selfHarnessSignals),
		// Fleet-wide failure clusters (Self-Harness weakness mining) — the same
		// evidence bundle the sweep nudge quotes, so a turn drilling in via
		// status sees the full support-ordered list, not just the top slice.
		FailureClusters: lifecycleValue(failureClusters),
		// Synthetic exercise lane liveness — the workout lane otherwise surfaces
		// only indirectly (workout-failure clusters), so this is its "is it
		// running" line in the integrated status view.
		WorkoutActivity: lifecycleValue(workoutActivity),
	}
	common.addToStatus(&status)
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

func (s skillLifecycleCommonStatus) addToStatus(status *chattools.SkillLifecycleStatusResult) {
	status.RejectedEdits = lifecycleValue(s.rejectedEdits)
	status.RejectedEditsError = s.rejectedEditsErr
	status.UsageQuality = lifecycleValue(s.usageQuality)
	status.UsageQualityError = s.usageQualityErr
	status.ValidationCases = lifecycleValue(s.validationCases)
	status.ValidationCasesError = s.validationCasesErr
	status.ValidationCaseSummary = lifecycleValue(s.validationSummary)
	status.ValidationCaseSummaryError = s.validationErr
	status.Opportunities = lifecycleValue(s.opportunities)
	status.OpportunitiesError = s.opportunitiesErr
	status.SelfCorrectionCandidates = lifecycleValue(s.selfCorrections)
	status.SelfCorrectionCandidatesError = s.selfCorrectionsErr
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

func propusUnavailableOverview(skillName string) chattools.SkillLifecycleOverview {
	scope := "global"
	if strings.TrimSpace(skillName) != "" {
		scope = "skill"
	}
	return chattools.SkillLifecycleOverview{
		Unavailable: &chattools.SkillLifecycleUnavailableOverview{
			State:       "unavailable",
			Scope:       scope,
			SkillName:   strings.TrimSpace(skillName),
			NextActions: []string{"configure_skill_tracker"},
		},
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
) chattools.SkillLifecycleOverview {
	overview := propus.BuildPropusOverview(propus.PropusOverviewInput{
		Scope:             propus.PropusScopeSkill,
		SkillName:         skillName,
		Recent:            propusview.LifecycleEntries(recent),
		SkillStats:        propusview.UsageStat(stats),
		Curator:           propusview.Curator(curator),
		UsageQuality:      propusview.UsageQuality(usageQuality),
		ValidationSummary: propusview.Validation(validationSummary),
		Opportunities:     propusview.Opportunities(opportunities),
		SelfCorrections:   propusview.SelfCorrections(selfCorrections),
	})
	return chattools.SkillLifecycleOverview{Operational: &overview}
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
) chattools.SkillLifecycleOverview {
	overview := propus.BuildPropusOverview(propus.PropusOverviewInput{
		Scope:              propus.PropusScopeGlobal,
		Recent:             propusview.LifecycleEntries(recent),
		Stats:              propusview.UsageStats(stats),
		Curator:            propusview.Curator(curator),
		UsageQuality:       propusview.UsageQuality(usageQuality),
		ValidationSummary:  propusview.Validation(validationSummary),
		Opportunities:      propusview.Opportunities(opportunities),
		SelfCorrections:    propusview.SelfCorrections(selfCorrections),
		SelfHarnessSignals: propusview.SelfHarness(selfHarnessSignals),
	})
	return chattools.SkillLifecycleOverview{Operational: &overview}
}

func propusSystemStatus(skillName string) propus.PropusSystemIdentity {
	scope := "global"
	if strings.TrimSpace(skillName) != "" {
		scope = "skill"
	}
	return propus.BuildPropusSystemIdentity(scope)
}
