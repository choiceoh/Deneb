package propusview

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
)

// Scope values for LifecycleSummaryInput.Scope, mirroring
// propus.PropusScopeGlobal/PropusScopeSkill so callers don't need to import
// propus directly just to pick a scope.
const (
	ScopeGlobal = propus.PropusScopeGlobal
	ScopeSkill  = propus.PropusScopeSkill
)

// LifecycleSummaryInput carries lifecycle history and live skill state using
// genesis-domain types, so callers can build the Propus lifecycle digest
// without importing the propus package themselves.
type LifecycleSummaryInput struct {
	Scope              string
	SkillName          string
	Recent             []genesis.LifecycleLogEntry
	Stats              []genesis.UsageStats
	Curator            []genesis.SkillCuratorRecord
	ValidationSummary  genesis.SkillValidationCaseSummary
	Opportunities      []genesis.SkillOpportunityRecord
	SelfCorrections    []genesis.SelfCorrectionCandidateRecord
	SelfHarnessSignals genesis.SelfHarnessSignalSummary
}

// BuildLifecycleSummary wraps propus.BuildPropusLifecycleSummary behind the
// genesis-typed input above. Callers depend on propusview (already needed for
// the field-level projections) instead of taking a direct propus import.
func BuildLifecycleSummary(input LifecycleSummaryInput) propus.PropusLifecycleSummary {
	return propus.BuildPropusLifecycleSummary(propus.PropusLifecycleSummaryInput{
		Scope:              input.Scope,
		SkillName:          input.SkillName,
		Recent:             LifecycleEntries(input.Recent),
		Stats:              UsageStats(input.Stats),
		Curator:            Curator(input.Curator),
		ValidationSummary:  Validation(input.ValidationSummary),
		Opportunities:      Opportunities(input.Opportunities),
		SelfCorrections:    SelfCorrections(input.SelfCorrections),
		SelfHarnessSignals: SelfHarness(input.SelfHarnessSignals),
	})
}
