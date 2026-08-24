package genesis

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// UsageQualitySummary explains why raw usage-log counts can differ from the
// evolver's success-rate stats. It is intentionally small and health-friendly:
// operators need to see when review/infra/no-evidence legacy records are being
// filtered instead of misreading a low or suddenly improved success rate.
type UsageQualitySummary struct {
	SkillName                                     string `json:"skillName,omitempty"`
	TotalRecords                                  int    `json:"totalRecords"`
	CountedRecords                                int    `json:"countedRecords"`
	IgnoredRecords                                int    `json:"ignoredRecords"`
	IgnoredReviewRecords                          int    `json:"ignoredReviewRecords,omitempty"`
	IgnoredConsultInfraFailures                   int    `json:"ignoredConsultInfraFailures,omitempty"`
	IgnoredUnactionableLegacyFailures             int    `json:"ignoredUnactionableLegacyFailures,omitempty"`
	TopIgnoredUnactionableLegacyFailureSkill      string `json:"topIgnoredUnactionableLegacyFailureSkill,omitempty"`
	TopIgnoredUnactionableLegacyFailureSkillCount int    `json:"topIgnoredUnactionableLegacyFailureSkillCount,omitempty"`
	// FailureLayers splits counted real-use failures by where on the skill's
	// path the run actually broke. ADVISORY: nothing here feeds a gate yet
	// (see SkillFailureLayers).
	FailureLayers SkillFailureLayers `json:"failureLayers"`
}

// SkillFailureLayers decomposes counted real-use records by the layer the
// outcome belongs to (Demystifying Agent Skills, 2608.14036).
//
// The problem it measures: every consult is written with the WHOLE run's
// success, so "skill X fails 40% of the time" has never distinguished
//
//	(a) the skill was loaded and then ignored — the run failed elsewhere, and
//	(b) the skill's procedure ran and did not work.
//
// Only (b) is evidence about the skill body. Counting (a) as skill failure is
// the same mis-attribution that already poisoned the held-out validation
// corpus, and it is what makes a curation verdict of "usage 0 / mostly fails"
// unsafe to act on.
//
// ADVISORY ONLY, deliberately: the evolver's success-rate gate is untouched by
// this split. Re-weighting a gate on a signal with no production history is
// how evolve thrash starts (PR #2328); the split has to accumulate real
// records first, and the numbers here are what will justify — or refute —
// gating on it later.
type SkillFailureLayers struct {
	// UnexercisedFailures ran to failure with NONE of the skill's declared
	// tools invoked: the body cannot be blamed on this evidence.
	UnexercisedFailures int `json:"unexercisedFailures"`
	// ExercisedFailures ran at least one declared tool and still failed: the
	// only failures that are genuinely about the procedure.
	ExercisedFailures int `json:"exercisedFailures"`
	// UnattributableFailures come from skills declaring no tools (or records
	// written before attribution existed) — unknown, not innocent.
	UnattributableFailures int `json:"unattributableFailures"`
	// AutoLoadFailures / ModelReadFailures split the same failures by how the
	// skill reached the turn: a skill that only ever fails when the model
	// chose it has a discovery problem, not a body problem.
	AutoLoadFailures  int `json:"autoLoadFailures"`
	ModelReadFailures int `json:"modelReadFailures"`
}

// UsageQualitySummary reports how many usage-log records are counted versus
// filtered out of evolver statistics. skillName="" returns a global summary.
func (t *Tracker) UsageQualitySummary(skillName string) (UsageQualitySummary, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	filter := strings.TrimSpace(skillName)
	summary := UsageQualitySummary{SkillName: filter}
	ignoredLegacyBySkill := map[string]int{}

	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return summary, fmt.Errorf("genesis-tracker: load usage quality: %w", err)
	}
	for _, r := range records {
		if filter != "" && r.SkillName != filter {
			continue
		}
		summary.TotalRecords++
		switch {
		case isReviewUsageRecord(r):
			summary.IgnoredReviewRecords++
		case !r.Success && isConsultInfraError(r.ErrorMsg):
			summary.IgnoredConsultInfraFailures++
		case isUnactionableLegacyFailure(r):
			summary.IgnoredUnactionableLegacyFailures++
			ignoredLegacyBySkill[r.SkillName]++
		default:
			summary.CountedRecords++
			summary.FailureLayers.observe(r)
		}
	}
	summary.IgnoredRecords = summary.TotalRecords - summary.CountedRecords
	for skill, count := range ignoredLegacyBySkill {
		if count > summary.TopIgnoredUnactionableLegacyFailureSkillCount {
			summary.TopIgnoredUnactionableLegacyFailureSkill = skill
			summary.TopIgnoredUnactionableLegacyFailureSkillCount = count
		}
	}
	return summary, nil
}

// observe folds one counted record into the layer split. Successes carry no
// failure to locate, so only failures are counted.
func (l *SkillFailureLayers) observe(r UsageRecord) {
	if r.Success {
		return
	}
	switch r.Exercised {
	case UsageExercisedYes:
		l.ExercisedFailures++
	case UsageExercisedNo:
		l.UnexercisedFailures++
	default:
		l.UnattributableFailures++
	}
	switch r.Delivery {
	case UsageDeliveryAutoLoad:
		l.AutoLoadFailures++
	case UsageDeliveryModelRead:
		l.ModelReadFailures++
	}
}
