package chatport

import "context"

// SkillNudger receives compact run snapshots for background skill-review
// decisions without exposing chat implementation types.
type SkillNudger interface {
	Enabled() bool
	OnToolCalls(ctx context.Context, sessionKey string, delta int, snapshot SkillNudgeSnapshot)
	Reset(sessionKey string)
}

// SkillNudgeSnapshot is the stable projection needed by a skill nudger.
type SkillNudgeSnapshot struct {
	Turns          int
	ToolActivities []SkillNudgeToolActivity
	AllText        string
	Label          string
	Model          string
}

// SkillNudgeToolActivity is the per-tool portion of a nudge snapshot.
type SkillNudgeToolActivity struct {
	Name    string
	IsError bool
}

// SkillUsageRecorder records one skill consultation outcome.
type SkillUsageRecorder interface {
	RecordSkillUse(sessionKey, skillName string, success bool, errMsg, model string)
}

// Skill delivery paths — HOW the skill reached the turn.
const (
	SkillDeliveryAutoLoad  = "auto-load"  // an exact trigger match injected the body
	SkillDeliveryModelRead = "model-read" // the model chose to read the skill
)

// Skill exercise verdicts — whether the turn shows the skill's procedure ran.
const (
	SkillExercisedYes     = "yes"     // a tool the skill requires actually ran
	SkillExercisedNo      = "no"      // none of its required tools ran
	SkillExercisedUnknown = "unknown" // the skill declares no tools to check against
)

// SkillUseAttribution says WHERE in the skill's path a turn's outcome belongs.
// A consult is recorded with the whole run's success, so without this a skill
// that was merely loaded and then ignored takes the blame for the turn — the
// mis-attribution that poisons the held-out validation corpus.
type SkillUseAttribution struct {
	Delivery  string
	Exercised string
}

// SkillUsageAttributionRecorder is the optional richer form of
// SkillUsageRecorder. Recorders that do not implement it keep the plain path.
type SkillUsageAttributionRecorder interface {
	RecordSkillUseAttributed(sessionKey, skillName string, success bool, errMsg, model string, attr SkillUseAttribution)
}
