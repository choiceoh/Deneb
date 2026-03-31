package chat

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func toolGmail(deps chattools.GmailPipelineDeps) ToolFunc {
	return chattools.ToolGmail(deps)
}
