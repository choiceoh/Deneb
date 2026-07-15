package propuswire

import (
	"strings"

	// scope constants re-exported for status assembly

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/propusview"
)

const (
	ScopeSkill  = propus.PropusScopeSkill
	ScopeGlobal = propus.PropusScopeGlobal
)

func UnavailableOverview(skillName string) chattools.SkillLifecycleOverview {
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

func SkillOverview(
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

func GlobalOverview(
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

func SystemStatus(skillName string) propus.PropusSystemIdentity {
	scope := "global"
	if strings.TrimSpace(skillName) != "" {
		scope = "skill"
	}
	return propus.BuildPropusSystemIdentity(scope)
}
