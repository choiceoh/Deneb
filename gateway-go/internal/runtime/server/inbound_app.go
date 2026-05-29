package server

import (
	"context"
	"strconv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// handleAppCommand replies to /app with a one-button URL message that
// launches the Mini App. We use a URL button instead of a web_app
// inline button because Bot API restricts InlineKeyboardButton.web_app
// to private chats — in supergroups Telegram returns "Text buttons are
// unallowed in the inline keyboard" and the button is rejected. The
// t.me/<bot>?startapp=... link is Telegram's official path for opening
// a Main Mini App from any chat (groups + channels included), so it
// works inside forum topics where /app actually adds value.
//
// PREREQUISITE: ?startapp only launches the app when the operator has
// enabled a Main Mini App in BotFather (Bot Settings -> Configure Mini
// App). The webAppURL config alone powers the menu button via
// setChatMenuButton, NOT this link — without the Main Mini App the link
// just opens the bot chat. The presence check below can only confirm
// webAppURL is set, not that the Main Mini App is configured, so the
// setup is documented in docs/operations/cloudflare-tunnel-setup.md
// ("Enable the Main Mini App").
//
// Refuses with a brief explanation when prerequisites are missing
// (bot username not yet resolved by getMe, or no Mini App configured)
// rather than sending a button to a broken URL.
func (p *InboundProcessor) handleAppCommand(chatID, threadID string) {
	plug := p.server.telegramPlug
	if plug == nil || plug.WebAppURL() == "" {
		p.sendCommandReply(chatID, threadID, &handlers.CommandResult{
			Reply: "⚠️ Mini App URL 이 설정되어 있지 않습니다. 운영자가 telegram.webAppURL 을 설정해주세요.",
		})
		return
	}
	bot := plug.BotUser()
	if bot == nil || bot.Username == "" {
		p.sendCommandReply(chatID, threadID, &handlers.CommandResult{
			Reply: "⚠️ 봇 username 을 아직 확인하지 못했습니다. 잠시 후 다시 시도해주세요.",
		})
		return
	}

	client := plug.Client()
	if client == nil {
		p.logger.Warn("telegram client not available for /app command")
		return
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}
	tid, _ := strconv.ParseInt(threadID, 10, 64)

	// Main Mini App launch URL. startapp param is required for Telegram
	// to recognize the link as a Mini App launcher (vs a plain bot DM
	// link); the value itself is opaque to deneb — present so we can
	// add deep-link context later (e.g. ?startapp=topic-42) without
	// changing the slash dispatch.
	launchURL := "https://t.me/" + bot.Username + "?startapp=open"
	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{
			{Text: "🚀 Mini App 열기", URL: launchURL},
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
