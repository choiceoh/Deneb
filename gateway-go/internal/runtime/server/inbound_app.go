package server

import (
	"context"
	"strconv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// handleAppCommand replies to /app with a one-button inline message that
// launches the Mini App. Inline web_app buttons work in any chat type
// (1:1, group, supergroup, topic) — unlike setChatMenuButton, which
// Telegram restricts to private chats. So /app is how the operator gets
// at the Mini App from inside a forum topic without first bouncing back
// to the bot's DM.
//
// Refuses with a brief explanation if no Mini App is configured —
// silently sending a button to about:blank would just confuse the user.
func (p *InboundProcessor) handleAppCommand(chatID, threadID string) {
	url := ""
	if plug := p.server.telegramPlug; plug != nil {
		url = plug.WebAppURL()
	}
	if url == "" {
		p.sendCommandReply(chatID, threadID, &handlers.CommandResult{
			Reply: "⚠️ Mini App URL 이 설정되어 있지 않습니다. 운영자가 telegram.webAppURL 을 설정해주세요.",
		})
		return
	}

	client := p.server.telegramPlug.Client()
	if client == nil {
		p.logger.Warn("telegram client not available for /app command")
		return
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}
	tid, _ := strconv.ParseInt(threadID, 10, 64)

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{
			{Text: "🚀 Mini App 열기", WebApp: &telegram.WebAppInfo{URL: url}},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := telegram.SendText(ctx, client, id, "👇 Mini App 을 여세요", telegram.SendOptions{
		ParseMode: "HTML",
		Keyboard:  keyboard,
		ThreadID:  tid,
	}); err != nil {
		p.logger.Warn("failed to send /app reply", "chatId", chatID, "error", err)
	}
}
