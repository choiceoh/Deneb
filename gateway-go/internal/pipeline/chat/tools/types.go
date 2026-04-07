package tools

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"

// ToolFunc is a type alias for the canonical definition in toolctx/.
// This eliminates the need for the adaptTool bridge between packages.
type ToolFunc = toolctx.ToolFunc
