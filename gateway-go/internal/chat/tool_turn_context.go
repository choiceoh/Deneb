package chat

import "github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"

// Type aliases — canonical definitions are in toolctx/.

type TurnContext = toolctx.TurnContext
type ToolTimingStats = toolctx.ToolTimingStats

// TurnResult was previously named turnResult_ (unexported). Now exported via toolctx.
type TurnResult = toolctx.TurnResult

func NewTurnContext() *TurnContext { return toolctx.NewTurnContext() }

func DetectCycle(refs map[string]string) error { return toolctx.DetectCycle(refs) }
