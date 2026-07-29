package skilllifecycle

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/propuswire"
)

// Skill-lifecycle status surface split out of skill_lifecycle_tool.go (pure
// move, no behavior change): per-skill/global status assembly and the Propus
// overview/doctrine DTOs.

type skillLifecycleStatusCacheKey struct {
	SkillName string
	Limit     int
}

type skillLifecycleStatusCacheEntry struct {
	Revision string
	Result   chattools.SkillLifecycleStatusResult
}

// SkillLifecycleStatus returns the current skill-lifecycle status.
func (b *skillLifecycleBackend) SkillLifecycleStatus(ctx context.Context, req chattools.SkillLifecycleStatusRequest) (chattools.SkillLifecycleStatusResult, error) {
	if b.tracker == nil {
		return chattools.SkillLifecycleStatusResult{
			System:   propuswire.SystemStatus(strings.TrimSpace(req.SkillName)),
			Overview: propuswire.UnavailableOverview(strings.TrimSpace(req.SkillName)),
			Reason:   "skill tracker is not configured",
		}, nil
	}

	limit := normalizeSkillLifecycleStatusLimit(req.Limit)
	skillName := strings.TrimSpace(req.SkillName)
	key := skillLifecycleStatusCacheKey{SkillName: skillName, Limit: limit}
	revision := b.tracker.StatusRevision()
	if cached, ok := b.cachedSkillLifecycleStatus(key, revision); ok {
		return cached, nil
	}

	status, err := b.skillLifecycleStatusUncached(ctx, skillName, limit)
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	if revision != "" && b.tracker.StatusRevision() == revision {
		b.storeSkillLifecycleStatusCache(key, revision, status)
	}
	return status, nil
}

func (b *skillLifecycleBackend) skillLifecycleStatusUncached(_ context.Context, skillName string, limit int) (chattools.SkillLifecycleStatusResult, error) {
	recent, err := b.tracker.RecentLifecycleLog(limit)
	if err != nil {
		return chattools.SkillLifecycleStatusResult{}, err
	}
	if skillName != "" {
		return b.skillLifecycleStatusForSkill(skillName, limit, recent)
	}
	return b.globalSkillLifecycleStatus(limit, recent)
}

func (b *skillLifecycleBackend) cachedSkillLifecycleStatus(key skillLifecycleStatusCacheKey, revision string) (chattools.SkillLifecycleStatusResult, bool) {
	if revision == "" {
		return chattools.SkillLifecycleStatusResult{}, false
	}
	b.statusCacheMu.Lock()
	defer b.statusCacheMu.Unlock()
	entry, ok := b.statusCache[key]
	if !ok || entry.Revision != revision {
		return chattools.SkillLifecycleStatusResult{}, false
	}
	return entry.Result, true
}

func (b *skillLifecycleBackend) storeSkillLifecycleStatusCache(key skillLifecycleStatusCacheKey, revision string, result chattools.SkillLifecycleStatusResult) {
	if revision == "" {
		return
	}
	b.statusCacheMu.Lock()
	defer b.statusCacheMu.Unlock()
	if b.statusCache == nil || len(b.statusCache) > 32 {
		b.statusCache = make(map[skillLifecycleStatusCacheKey]skillLifecycleStatusCacheEntry)
	}
	b.statusCache[key] = skillLifecycleStatusCacheEntry{Revision: revision, Result: result}
}

func (b *skillLifecycleBackend) clearSkillLifecycleStatusCache() {
	b.statusCacheMu.Lock()
	defer b.statusCacheMu.Unlock()
	b.statusCache = nil
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
		System:          propuswire.SystemStatus(skillName),
		Overview:        propuswire.SkillOverview(skillName, recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections),
		OK:              true,
		SkillName:       skillName,
		Limit:           lifecycleValue(limit),
		Recent:          lifecycleValue(recent),
		Stats:           &chattools.SkillLifecycleStats{Scope: propuswire.ScopeSkill, Skill: stats},
		Curator:         lifecycleValue(curator),
		OptimizerMemory: lifecycleValue(optimizerMemory),
	}
	common.addToStatus(&status)
	status.OptimizerMemoryError = optimizerMemoryErr
	slimSkillLifecycleStatusForAgent(&status)
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
		System:             propuswire.SystemStatus(""),
		Overview:           propuswire.GlobalOverview(recent, stats, curator, common.usageQuality, common.validationSummary, common.opportunities, common.selfCorrections, selfHarnessSignals),
		OK:                 true,
		Limit:              lifecycleValue(limit),
		Recent:             lifecycleValue(recent),
		Stats:              &chattools.SkillLifecycleStats{Scope: propuswire.ScopeGlobal, Fleet: stats},
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
	slimSkillLifecycleStatusForAgent(&status)
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
	ablationSummary    genesis.SkillAblationSummary
	ablationErr        string
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
	ablationSummary, ablationErr := b.skillAblationSummary(skillName)
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
		ablationSummary:    ablationSummary,
		ablationErr:        ablationErr,
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
	status.AblationSummary = lifecycleValue(s.ablationSummary)
	status.AblationSummaryError = s.ablationErr
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

func (b *skillLifecycleBackend) skillAblationSummary(skillName string) (genesis.SkillAblationSummary, string) {
	summary, err := b.tracker.SkillAblationSummary(skillName)
	if err == nil {
		return summary, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: ablation summary unavailable",
			"skill", skillName, "error", err)
	}
	return genesis.SkillAblationSummary{SkillName: strings.TrimSpace(skillName)}, err.Error()
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
	// Open backlog only: proposed (awaiting review) + accepted (L4 dispatchable).
	// Filtering to proposed alone hid the accepted code backlog that the
	// heartbeat review lane endorses for coding-dispatch (observed 2026-07-13).
	if limit <= 0 {
		limit = 20
	}
	// Over-fetch so a burst of rejected/applied rows cannot crowd out open ones.
	all, err := b.tracker.RecentSelfCorrectionCandidates(skillName, "", limit*4)
	if err != nil {
		if b.logger != nil {
			b.logger.Warn("skill lifecycle: self-correction candidates unavailable",
				"skill", skillName, "error", err)
		}
		return []genesis.SelfCorrectionCandidateRecord{}, err.Error()
	}
	out := make([]genesis.SelfCorrectionCandidateRecord, 0, limit)
	for _, rec := range all {
		switch rec.Status {
		case genesis.SelfCorrectionStatusProposed, genesis.SelfCorrectionStatusAccepted:
			out = append(out, rec)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, ""
}
