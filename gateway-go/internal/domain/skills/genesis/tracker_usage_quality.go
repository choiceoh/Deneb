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
