package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/llm"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func toolSubagents(d *SessionDeps) ToolFunc { return chattools.ToolSubagents(d) }
func toolImage(client *llm.Client) ToolFunc { return chattools.ToolImage(client) }
