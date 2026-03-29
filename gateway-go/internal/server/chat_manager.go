package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler        *chat.Handler
	toolDeps           *chat.CoreToolDeps
	telegramPlug       *telegram.Plugin
	discordPlug        *discord.Plugin
	discordThreadNamer *discord.ThreadNamer // optional; nil when disabled or no Anthropic creds
}
