// Package propusview adapts genesis persistence records to Propus read models.
package propusview

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
)

// LifecycleEntries projects persisted lifecycle entries for Propus summaries.
func LifecycleEntries(entries []genesis.LifecycleLogEntry) []propus.LifecycleLogEntry {
	out := make([]propus.LifecycleLogEntry, 0, len(entries))
	for _, entry := range entries {
		item := propus.LifecycleLogEntry{
			Type:      entry.Type,
			SkillName: entry.SkillName,
			CreatedAt: entry.CreatedAt,
			Executed:  entry.Executed,
		}
		if audit := entry.SelfHarnessAudit; audit != nil {
			item.SelfHarnessAudit = &propus.HarnessEditAudit{
				TargetSignature:        audit.TargetSignature,
				EditedSurface:          audit.EditedSurface,
				ExpectedBehaviorChange: audit.ExpectedBehaviorChange,
				RegressionRisk:         audit.RegressionRisk,
				PrimaryDimension:       audit.PrimaryDimension,
				SecondaryDimensions:    append([]string(nil), audit.SecondaryDimensions...),
			}
		}
		out = append(out, item)
	}
	return out
}

// UsageStats projects skill usage statistics.
func UsageStats(stats []genesis.UsageStats) []propus.UsageStats {
	out := make([]propus.UsageStats, 0, len(stats))
	for _, stat := range stats {
		out = append(out, propus.UsageStats{
			SkillName:    stat.SkillName,
			TotalUses:    stat.TotalUses,
			SuccessCount: stat.SuccessCount,
			FailureCount: stat.FailureCount,
			SuccessRate:  stat.SuccessRate,
		})
	}
	return out
}

// UsageStat projects one optional skill usage statistic.
func UsageStat(stat *genesis.UsageStats) *propus.UsageStats {
	if stat == nil {
		return nil
	}
	return &propus.UsageStats{
		SkillName: stat.SkillName, TotalUses: stat.TotalUses, SuccessCount: stat.SuccessCount,
		FailureCount: stat.FailureCount, SuccessRate: stat.SuccessRate,
	}
}

// Curator projects lifecycle curator records.
func Curator(records []genesis.SkillCuratorRecord) []propus.SkillCuratorRecord {
	out := make([]propus.SkillCuratorRecord, 0, len(records))
	for _, record := range records {
		out = append(out, propus.SkillCuratorRecord{SkillName: record.SkillName, CreatedBy: record.CreatedBy, State: record.State})
	}
	return out
}

// UsageQuality projects usage filtering counts plus the success-side bypass
// split. Actionable() is evaluated here rather than in propus so the evidence
// and rate floors stay next to the evidence that defines them.
func UsageQuality(summary genesis.UsageQualitySummary) propus.UsageQualitySummary {
	return propus.UsageQualitySummary{
		TotalRecords: summary.TotalRecords, CountedRecords: summary.CountedRecords, IgnoredRecords: summary.IgnoredRecords,
		BypassedSuccesses:   summary.Bypass.BypassedSuccesses,
		AttributedSuccesses: summary.Bypass.AttributedSuccesses(),
		BypassActionable:    summary.Bypass.Actionable(),
	}
}

// Validation projects held-out validation corpus counts.
func Validation(summary genesis.SkillValidationCaseSummary) propus.SkillValidationCaseSummary {
	return propus.SkillValidationCaseSummary{
		SkillName: summary.SkillName, UniqueRecords: summary.UniqueRecords, SkillsWithCases: summary.SkillsWithCases,
		UniqueEasyAnchorCases: summary.UniqueEasyAnchorCases, UniqueMixedFrontierCases: summary.UniqueMixedFrontierCases,
		UniqueHardFrontierCases: summary.UniqueHardFrontierCases,
	}
}

// Opportunities projects open strategy-map items.
func Opportunities(records []genesis.SkillOpportunityRecord) []propus.SkillOpportunityRecord {
	out := make([]propus.SkillOpportunityRecord, 0, len(records))
	for _, record := range records {
		out = append(out, propus.SkillOpportunityRecord{SkillName: record.SkillName, Route: record.Route})
	}
	return out
}

// SelfCorrections projects deferred self-correction candidates.
func SelfCorrections(records []genesis.SelfCorrectionCandidateRecord) []propus.SelfCorrectionCandidateRecord {
	out := make([]propus.SelfCorrectionCandidateRecord, 0, len(records))
	for _, record := range records {
		out = append(out, propus.SelfCorrectionCandidateRecord{SkillName: record.SkillName, Status: record.Status})
	}
	return out
}

// SelfHarness projects gate and recurrence signals.
func SelfHarness(summary genesis.SelfHarnessSignalSummary) propus.SelfHarnessSignalSummary {
	return propus.SelfHarnessSignalSummary{
		Rejections7d: summary.Rejections7d, ValidationDrafts7d: summary.ValidationDrafts7d,
		TargetRecurrences7d: summary.TargetRecurrences7d, SignatureMismatchRejections7d: summary.SignatureMismatchRejections7d,
		SurfaceMismatchRejections7d: summary.SurfaceMismatchRejections7d,
	}
}

// Liveness projects persisted Propus activity state.
func Liveness(state genesis.SkillLivenessState) propus.SkillLivenessState {
	return propus.SkillLivenessState{
		LastReviewAt: state.LastReviewAt, LastEvolveAt: state.LastEvolveAt, LastGenesisAt: state.LastGenesisAt,
		LastErrorAt: state.LastErrorAt, LastError: state.LastError, ReviewAttempts: state.ReviewAttempts,
		ReviewSkips: state.ReviewSkips, ValidationRejections: state.ValidationRejections,
	}
}

// Evolution projects recent evolution outcomes.
func Evolution(summary genesis.EvolutionHealthSummary) propus.EvolutionHealthSummary {
	return propus.EvolutionHealthSummary{
		Thrash: summary.Thrash, EvolveRolledBack7d: summary.EvolveRolledBack7d, EvolveRejected7d: summary.EvolveRejected7d,
	}
}
