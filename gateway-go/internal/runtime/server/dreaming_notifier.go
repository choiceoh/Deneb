package server

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// Compile-time interface compliance.
var (
	_ autonomous.Notifier = (*telegramNotifier)(nil)
	_ gmailpoll.Notifier  = (*telegramNotifier)(nil)
)

// telegramNotifier implements autonomous.Notifier by sending DMs via Telegram.
// Used for AuroraDream notifications.
type telegramNotifier struct {
	plugin *telegram.Plugin
	chatID int64
	logger *slog.Logger
}

// Notify sends a plain-text notification to the paired Telegram chat.
func (n *telegramNotifier) Notify(ctx context.Context, message string) error {
	client := n.plugin.Client()
	if client == nil {
		return nil // Telegram not connected yet; silently skip.
	}
	_, err := telegram.SendText(ctx, client, n.chatID, message, telegram.SendOptions{})
	if err != nil {
		n.logger.Warn("dreaming telegram notification failed", "error", err)
	}
	return err
}
