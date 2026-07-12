package briefcase

import feedbackcontract "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/feedback"

const (
	FeedbackSchemaVersion          = feedbackcontract.FeedbackSchemaVersion
	UserSimulatorPlanSchemaVersion = feedbackcontract.UserSimulatorPlanSchemaVersion

	VerdictSatisfactory  = feedbackcontract.VerdictSatisfactory
	VerdictNeedsRevision = feedbackcontract.VerdictNeedsRevision
	VerdictBlocked       = feedbackcontract.VerdictBlocked
	VerdictCannotAssess  = feedbackcontract.VerdictCannotAssess

	ScoreBandHigh        = feedbackcontract.ScoreBandHigh
	ScoreBandMedium      = feedbackcontract.ScoreBandMedium
	ScoreBandLow         = feedbackcontract.ScoreBandLow
	ScoreBandUnavailable = feedbackcontract.ScoreBandUnavailable

	ArtifactAvailable  = feedbackcontract.ArtifactAvailable
	ArtifactMissing    = feedbackcontract.ArtifactMissing
	ArtifactUnreadable = feedbackcontract.ArtifactUnreadable

	ForbiddenSealedSource        = feedbackcontract.ForbiddenSealedSource
	ForbiddenSealedPath          = feedbackcontract.ForbiddenSealedPath
	ForbiddenHiddenReference     = feedbackcontract.ForbiddenHiddenReference
	ForbiddenRubricID            = feedbackcontract.ForbiddenRubricID
	ForbiddenCheckpointID        = feedbackcontract.ForbiddenCheckpointID
	ForbiddenSupervisorReasoning = feedbackcontract.ForbiddenSupervisorReasoning
	ForbiddenHiddenRationale     = feedbackcontract.ForbiddenHiddenRationale
	ForbiddenExpectedAnswer      = feedbackcontract.ForbiddenExpectedAnswer
	ForbiddenSupervisorMetadata  = feedbackcontract.ForbiddenSupervisorMetadata
	ForbiddenExplicitSensitive   = feedbackcontract.ForbiddenExplicitSensitive
	ForbiddenStructuralMarker    = feedbackcontract.ForbiddenStructuralMarker
)

var (
	ErrInvalidFeedback          = feedbackcontract.ErrInvalidFeedback
	ErrFeedbackLeak             = feedbackcontract.ErrFeedbackLeak
	ErrInvalidUserSimulatorPlan = feedbackcontract.ErrInvalidUserSimulatorPlan
)

type VerdictCategory = feedbackcontract.VerdictCategory
type ScoreBand = feedbackcontract.ScoreBand
type ArtifactSummaryStatus = feedbackcontract.ArtifactSummaryStatus
type HiddenFeedbackInputs = feedbackcontract.HiddenFeedbackInputs
type FeedbackLimits = feedbackcontract.FeedbackLimits
type VisibleArtifactSummary = feedbackcontract.VisibleArtifactSummary
type SimulatorHandoffInput = feedbackcontract.SimulatorHandoffInput
type SimulatorHandoff = feedbackcontract.SimulatorHandoff
type ForbiddenFeedbackClass = feedbackcontract.ForbiddenFeedbackClass
type FeedbackLeakError = feedbackcontract.FeedbackLeakError
type FeedbackFirewall = feedbackcontract.FeedbackFirewall
type UserSimulator = feedbackcontract.UserSimulator
type UserSimulatorPlan = feedbackcontract.UserSimulatorPlan
type ScriptedFollowUp = feedbackcontract.ScriptedFollowUp
type ScriptedUserSimulator = feedbackcontract.ScriptedUserSimulator

func NewFeedbackFirewall(hidden HiddenFeedbackInputs, limits FeedbackLimits) (*FeedbackFirewall, error) {
	return feedbackcontract.NewFeedbackFirewall(hidden, limits)
}

func NewScriptedUserSimulator(plan UserSimulatorPlan, authorizedMax int) (*ScriptedUserSimulator, error) {
	return feedbackcontract.NewScriptedUserSimulator(plan, authorizedMax)
}
