package surface

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/codesearchtool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/graphifyops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/messageops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/officeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/orgops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/recallops"
)

// Re-exports used by toolwire/core registration so that package stays at/under
// the fanout soft bar without importing the parent tools package.
var (
	ToolGraphify      = graphifyops.ToolGraphify
	ToolCodeSearch    = codesearchtool.ToolCodeSearch
	ToolOffice        = officeops.ToolOffice
	ToolOrg           = orgops.ToolOrg
	ToolMessage       = messageops.ToolMessage
	ToolResearchPanel = recallops.ToolResearchPanel
)
