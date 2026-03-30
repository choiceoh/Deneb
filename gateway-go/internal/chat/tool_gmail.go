package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/llm"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func toolGmail(llmClient *llm.Client, defaultModel string) ToolFunc {
	return chattools.ToolGmail(llmClient, defaultModel)
}
