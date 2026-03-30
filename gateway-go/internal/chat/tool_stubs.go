package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"

func toolGateway(repoDir string) ToolFunc { return chattools.ToolGateway(repoDir) }

// --- sessions_list tool ---
