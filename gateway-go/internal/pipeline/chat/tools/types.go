package tools

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"

// ToolFunc is a type alias for the canonical definition in toolport/.
// This eliminates the need for the adaptTool bridge between packages.
type ToolFunc = toolport.ToolFunc
