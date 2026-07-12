package tools

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"

// SkillLifecycleBackend executes Deneb's closed-loop skill lifecycle.
type SkillLifecycleBackend = lifecycletool.SkillLifecycleBackend

// HeartbeatShadowReplayResult reports a dry-run heartbeat comparison.
type HeartbeatShadowReplayResult = lifecycletool.HeartbeatShadowReplayResult

// HeartbeatShadowReplayFixtureResult is one heartbeat replay comparison.
type HeartbeatShadowReplayFixtureResult = lifecycletool.HeartbeatShadowReplayFixtureResult

// SkillEvolutionProposalResult reports a proposal and optional execution.
type SkillEvolutionProposalResult = lifecycletool.SkillEvolutionProposalResult

// SkillEvolutionProposalExecutionResult is the proposal execution result union.
type SkillEvolutionProposalExecutionResult = lifecycletool.SkillEvolutionProposalExecutionResult

// SkillGenesisResult reports a generated skill or intentional skip.
type SkillGenesisResult = lifecycletool.SkillGenesisResult

// SkillEvolutionResult reports one skill evolution attempt.
type SkillEvolutionResult = lifecycletool.SkillEvolutionResult

// SkillLifecycleStatusResult reports typed Propus lifecycle state.
type SkillLifecycleStatusResult = lifecycletool.SkillLifecycleStatusResult

// SkillLifecycleOverview wraps operational or unavailable Propus state.
type SkillLifecycleOverview = lifecycletool.SkillLifecycleOverview

// SkillLifecycleUnavailableOverview is the minimal unconfigured state.
type SkillLifecycleUnavailableOverview = lifecycletool.SkillLifecycleUnavailableOverview

// SkillLifecycleStats is the skill/global status stats union.
type SkillLifecycleStats = lifecycletool.SkillLifecycleStats

// SkillCuratorActionResult reports a curator transition.
type SkillCuratorActionResult = lifecycletool.SkillCuratorActionResult

// SkillCuratorResult is the single-record/list curator result union.
type SkillCuratorResult = lifecycletool.SkillCuratorResult

// SkillValidationCaseResult reports a recorded validation case.
type SkillValidationCaseResult = lifecycletool.SkillValidationCaseResult

// SkillValidationCaseFromSessionResult reports transcript-derived validation.
type SkillValidationCaseFromSessionResult = lifecycletool.SkillValidationCaseFromSessionResult

// SkillValidationBackfillResult reports transcript validation backfill.
type SkillValidationBackfillResult = lifecycletool.SkillValidationBackfillResult

// SkillValidationBackfillDetail is one scanned transcript outcome.
type SkillValidationBackfillDetail = lifecycletool.SkillValidationBackfillDetail

// SkillSelfCorrectionCandidateResult reports a queued correction.
type SkillSelfCorrectionCandidateResult = lifecycletool.SkillSelfCorrectionCandidateResult

// SkillSelfCorrectionReviewResult reports a correction review.
type SkillSelfCorrectionReviewResult = lifecycletool.SkillSelfCorrectionReviewResult

// HeartbeatShadowReplayRequest carries a heartbeat candidate for dry-run replay.
type HeartbeatShadowReplayRequest = lifecycletool.HeartbeatShadowReplayRequest

// SkillEvolutionProposalRequest records a skill evolution routing decision.
type SkillEvolutionProposalRequest = lifecycletool.SkillEvolutionProposalRequest

// SkillGenesisRequest triggers skill generation from a session or summary.
type SkillGenesisRequest = lifecycletool.SkillGenesisRequest

// SkillEvolutionRequest triggers improvement of an existing skill.
type SkillEvolutionRequest = lifecycletool.SkillEvolutionRequest

// SkillLifecycleStatusRequest queries lifecycle state and recent decisions.
type SkillLifecycleStatusRequest = lifecycletool.SkillLifecycleStatusRequest

// SkillSelfCorrectionCandidateRequest records a deferred correction candidate.
type SkillSelfCorrectionCandidateRequest = lifecycletool.SkillSelfCorrectionCandidateRequest

// SkillSelfCorrectionReviewRequest records review of a correction candidate.
type SkillSelfCorrectionReviewRequest = lifecycletool.SkillSelfCorrectionReviewRequest

// SkillCuratorActionRequest updates curator state for an agent-created skill.
type SkillCuratorActionRequest = lifecycletool.SkillCuratorActionRequest

// SkillValidationCaseRequest records a held-out skill invariant.
type SkillValidationCaseRequest = lifecycletool.SkillValidationCaseRequest

// SkillValidationCaseFromSessionRequest derives a validation case from a session.
type SkillValidationCaseFromSessionRequest = lifecycletool.SkillValidationCaseFromSessionRequest

// SkillValidationBackfillRequest derives validation cases from recent sessions.
type SkillValidationBackfillRequest = lifecycletool.SkillValidationBackfillRequest

// SkillReplayCaseRequest describes a dry-run task and its replay expectations.
type SkillReplayCaseRequest = lifecycletool.SkillReplayCaseRequest

// SkillReplayToolCallRequest describes one expected or forbidden tool call.
type SkillReplayToolCallRequest = lifecycletool.SkillReplayToolCallRequest

// ToolSkillLifecycle exposes the skill lifecycle actions as one tool.
var ToolSkillLifecycle = lifecycletool.ToolSkillLifecycle

// SkillLifecycleToolSchema returns the JSON schema for the lifecycle tool.
var SkillLifecycleToolSchema = lifecycletool.SkillLifecycleToolSchema
