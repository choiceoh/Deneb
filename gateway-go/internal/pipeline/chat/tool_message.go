package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"

func toolMessage() ToolFunc { return chattools.ToolMessage() }
