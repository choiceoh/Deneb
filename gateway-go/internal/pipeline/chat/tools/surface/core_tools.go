package surface

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// Re-exports of parent tools symbols used by toolwire/core registration so
// that package stays at/under the fanout soft bar without importing tools/.
var (
	ToolGraphify      = tools.ToolGraphify
	ToolCodeSearch    = tools.ToolCodeSearch
	ToolOffice        = tools.ToolOffice
	ToolGoal          = tools.ToolGoal
	ToolBlackboard    = tools.ToolBlackboard
	ToolOrg           = tools.ToolOrg
	ToolMessage       = tools.ToolMessage
	ToolResearchPanel = tools.ToolResearchPanel
	NewGoalGlanceFunc = tools.NewGoalGlanceFunc
	HandleGoalCommand = tools.HandleGoalCommand
)
