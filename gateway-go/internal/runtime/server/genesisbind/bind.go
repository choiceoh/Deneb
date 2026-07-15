// Package genesisbind re-exports genesis types for the server composition root.
// Type/var aliases only — no adapter logic.
package genesisbind

import (
	genesis "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// --- domain/skills/genesis ---

type (
	AdversarialCoverageTask       = genesis.AdversarialCoverageTask
	CurriculumTask                = genesis.CurriculumTask
	EvolutionTask                 = genesis.EvolutionTask
	EvolveResult                  = genesis.EvolveResult
	Evolver                       = genesis.Evolver
	FailureClusterSummary         = genesis.FailureClusterSummary
	HarnessEditAudit              = genesis.HarnessEditAudit
	JudgeAccuracyTask             = genesis.JudgeAccuracyTask
	LadderWatchTask               = genesis.LadderWatchTask
	LifecycleLogEntry             = genesis.LifecycleLogEntry
	MetaAdoptionHealth            = genesis.MetaAdoptionHealth
	MetaEvolutionTask             = genesis.MetaEvolutionTask
	MetaRevisionRecord            = genesis.MetaRevisionRecord
	OperatorJudgeVerdict          = genesis.OperatorJudgeVerdict
	RSILoopStatus                 = genesis.RSILoopStatus
	RuntimeErrorMiningTask        = genesis.RuntimeErrorMiningTask
	SelfCorrectionCandidateRecord = genesis.SelfCorrectionCandidateRecord
	SelfCorrectionFunnelSummary   = genesis.SelfCorrectionFunnelSummary
	SelfHarnessSignalSummary      = genesis.SelfHarnessSignalSummary
	SkillCuratorRecord            = genesis.SkillCuratorRecord
	SkillCuratorTask              = genesis.SkillCuratorTask
	SkillOpportunityRecord        = genesis.SkillOpportunityRecord
	SkillReplayCaseRecord         = genesis.SkillReplayCaseRecord
	SkillReplayToolCallRecord     = genesis.SkillReplayToolCallRecord
	SkillValidationCaseRecord     = genesis.SkillValidationCaseRecord
	SkillValidationCaseSummary    = genesis.SkillValidationCaseSummary
	SkillWorkoutTask              = genesis.SkillWorkoutTask
	UsageRecord                   = genesis.UsageRecord
	UsageStats                    = genesis.UsageStats
)

var (
	DefaultEvolveEventThreshold     = genesis.DefaultEvolveEventThreshold
	DefaultRollbackThreshold        = genesis.DefaultRollbackThreshold
	NewSkillValidationEngine        = genesis.NewSkillValidationEngine
	NewTracker                      = genesis.NewTracker
	OperatorJudgeVerdictConfirm     = genesis.OperatorJudgeVerdictConfirm
	OperatorJudgeVerdictRollback    = genesis.OperatorJudgeVerdictRollback
	SelfCorrectionStatusAccepted    = genesis.SelfCorrectionStatusAccepted
	SelfCorrectionStatusProposed    = genesis.SelfCorrectionStatusProposed
	SkillActivityReviewAttempt      = genesis.SkillActivityReviewAttempt
	SkillActivityReviewSkipped      = genesis.SkillActivityReviewSkipped
	SkillActivityValidationRejected = genesis.SkillActivityValidationRejected
	SkillCuratorConfigFromEnv       = genesis.SkillCuratorConfigFromEnv
	SourceAutoDispatches            = genesis.SourceAutoDispatches
)
