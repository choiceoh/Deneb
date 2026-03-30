package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"

func toolMorningLetter(exec ToolExecutor) ToolFunc { return chattools.ToolMorningLetter(exec) }
