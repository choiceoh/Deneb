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
