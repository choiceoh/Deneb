package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func toolAgentLogs(w *agentlog.Writer) ToolFunc { return chattools.ToolAgentLogs(w) }
