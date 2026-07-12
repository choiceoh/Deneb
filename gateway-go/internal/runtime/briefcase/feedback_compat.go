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

type (
	VerdictCategory        = feedbackcontract.VerdictCategory
	ScoreBand              = feedbackcontract.ScoreBand
	ArtifactSummaryStatus  = feedbackcontract.ArtifactSummaryStatus
	HiddenFeedbackInputs   = feedbackcontract.HiddenFeedbackInputs
	FeedbackLimits         = feedbackcontract.FeedbackLimits
	VisibleArtifactSummary = feedbackcontract.VisibleArtifactSummary
	SimulatorHandoffInput  = feedbackcontract.SimulatorHandoffInput
	SimulatorHandoff       = feedbackcontract.SimulatorHandoff
	ForbiddenFeedbackClass = feedbackcontract.ForbiddenFeedbackClass
	FeedbackLeakError      = feedbackcontract.FeedbackLeakError
	FeedbackFirewall       = feedbackcontract.FeedbackFirewall
	UserSimulator          = feedbackcontract.UserSimulator
	UserSimulatorPlan      = feedbackcontract.UserSimulatorPlan
	ScriptedFollowUp       = feedbackcontract.ScriptedFollowUp
	ScriptedUserSimulator  = feedbackcontract.ScriptedUserSimulator
)

func NewFeedbackFirewall(hidden HiddenFeedbackInputs, limits FeedbackLimits) (*FeedbackFirewall, error) {
	return feedbackcontract.NewFeedbackFirewall(hidden, limits)
}

func NewScriptedUserSimulator(plan UserSimulatorPlan, authorizedMax int) (*ScriptedUserSimulator, error) {
	return feedbackcontract.NewScriptedUserSimulator(plan, authorizedMax)
}
