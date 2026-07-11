package tools

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"

// SkillLifecycleBackend executes Deneb's closed-loop skill lifecycle.
type SkillLifecycleBackend = lifecycletool.SkillLifecycleBackend

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
