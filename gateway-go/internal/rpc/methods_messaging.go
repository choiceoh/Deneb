package rpc

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MessagingDeps holds dependencies for send/poll RPC methods.
type MessagingDeps struct {
	// TelegramPlugin is the native Telegram channel plugin (nil if not configured).
	TelegramPlugin *telegram.Plugin
}

// RegisterMessagingMethods registers the send and poll RPC methods.
// These use the native Telegram plugin for message delivery.
func RegisterMessagingMethods(d *Dispatcher, deps MessagingDeps) {
	d.Register("send", messagingSend(deps))
	d.Register("poll", messagingPoll(deps))
}

func messagingSend(deps MessagingDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Channel string `json:"channel"`
			To      string `json:"to"`
			Text    string `json:"text"`
			Media   []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"media,omitempty"`
			ReplyTo  int64 `json:"replyTo,omitempty"`
			ThreadID int64 `json:"threadId,omitempty"`
			Silent   bool  `json:"silent,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid send params: "+err.Error()))
		}

		// Route to Telegram if channel matches and plugin is available.
		if (p.Channel == "" || p.Channel == "telegram") && deps.TelegramPlugin != nil {
			client := deps.TelegramPlugin.Client()
			if client == nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "telegram client not connected"))
			}

			chatID, err := parseChatID(p.To)
			if err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrInvalidRequest, "invalid chat ID: "+err.Error()))
			}

			opts := telegram.SendOptions{
				ParseMode:           "HTML",
				ThreadID:            p.ThreadID,
				DisableNotification: p.Silent,
				ReplyToMessageID:    p.ReplyTo,
			}

			// Format text as HTML.
			html := telegram.FormatHTML(p.Text)

			results, err := telegram.SendText(ctx, client, chatID, html, opts)
			if err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrDependencyFailed, "telegram send failed: "+err.Error()))
			}

			// Send media attachments.
			for _, m := range p.Media {
				switch m.Type {
				case "photo", "image":
					_, _ = telegram.SendPhoto(ctx, client, chatID, m.URL, "", opts)
				case "document", "file":
					_, _ = telegram.SendDocument(ctx, client, chatID, m.URL, "", opts)
				case "video":
					_, _ = telegram.SendVideo(ctx, client, chatID, m.URL, "", opts)
				case "audio":
					_, _ = telegram.SendAudio(ctx, client, chatID, m.URL, "", opts)
				case "voice":
					_, _ = telegram.SendVoice(ctx, client, chatID, m.URL, "", opts)
				}
			}

			var resultData any
			if len(results) > 0 {
				resultData = results[0]
			}
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ok":      true,
				"channel": "telegram",
				"result":  resultData,
			})
			return resp
		}

		// No other channels available in standalone Go gateway.
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, "no channel available for sending"))
	}
}

func messagingPoll(deps MessagingDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// If Telegram plugin is running, drain buffered messages.
		if deps.TelegramPlugin != nil {
			bot := deps.TelegramPlugin.Bot()
			if bot != nil {
				messages := bot.DrainMessages()
				resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
					"channel":  "telegram",
					"messages": messages,
					"count":    len(messages),
				})
				return resp
			}
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"messages": []any{},
			"count":    0,
		})
		return resp
	}
}

func parseChatID(to string) (int64, error) {
	return strconv.ParseInt(to, 10, 64)
}
