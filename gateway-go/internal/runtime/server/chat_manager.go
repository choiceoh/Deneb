package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler   *chat.Handler
	toolDeps      *chat.CoreToolDeps
	telegramPlug  *telegram.Plugin
	modelRegistry *modelrole.Registry
	localAIHub    *localai.Hub
}
