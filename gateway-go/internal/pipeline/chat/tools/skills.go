package tools

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/skilltool"
)

// SkillsSnapshotProvider returns the current cached skills snapshot.
type SkillsSnapshotProvider = skilltool.SkillsSnapshotProvider

// SkillManageInvalidateFn is called after a skill mutation to invalidate the
// skills prompt cache when immediate application was requested.
type SkillManageInvalidateFn = skilltool.SkillManageInvalidateFn

// ToolSkills creates the unified skills tool.
var ToolSkills = skilltool.ToolSkills
