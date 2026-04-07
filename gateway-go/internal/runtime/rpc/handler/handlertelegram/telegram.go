// Package handlertelegram provides RPC handlers for Telegram lifecycle
// (start/stop/restart) and messaging (send/poll) methods.
package handlertelegram

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Deps structs
// ---------------------------------------------------------------------------

// LifecycleDeps holds the dependencies for Telegram lifecycle RPC methods
// (telegram.start, telegram.stop, telegram.restart).
type LifecycleDeps struct {
	TelegramPlugin *telegram.Plugin
	InternalHooks  *hooks.InternalRegistry
	Broadcaster    *events.Broadcaster
}

// MessagingDeps holds dependencies for send/poll RPC methods.
type MessagingDeps struct {
	// TelegramPlugin is the native Telegram channel plugin (nil if not configured).
	TelegramPlugin *telegram.Plugin
}

// ---------------------------------------------------------------------------
// Method registries
// ---------------------------------------------------------------------------

// LifecycleMethods returns Telegram start/stop/restart RPC handlers.
// Returns nil if TelegramPlugin is not configured.
func LifecycleMethods(deps LifecycleDeps) map[string]rpcutil.HandlerFunc {
	if deps.TelegramPlugin == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"telegram.start":   telegramStart(deps),
		"telegram.stop":    telegramStop(deps),
		"telegram.restart": telegramRestart(deps),
	}
}

// MessagingMethods returns the send and poll RPC handlers.
// These use the native Telegram plugin for message delivery.
func MessagingMethods(deps MessagingDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"send": messagingSend(deps),
		"poll": messagingPoll(deps),
	}
}

// ---------------------------------------------------------------------------
// Telegram lifecycle handlers
// ---------------------------------------------------------------------------

// emitTelegramLifecycleEvent fires the internal hook and broadcasts a
// telegram.changed event after a successful Telegram operation.
func emitTelegramLifecycleEvent(deps LifecycleDeps, id string, hookEvent hooks.Event, action string) {
	if deps.InternalHooks != nil {
		env := map[string]string{"DENEB_CHANNEL_ID": id}
		go func() {
			defer func() { recover() }()
			deps.InternalHooks.TriggerFromEvent(context.Background(), hookEvent, "", env)
		}()
	}
	if deps.Broadcaster != nil {
		deps.Broadcaster.Broadcast("telegram.changed", map[string]any{
			"channelId": id,
			"action":    action,
			"ts":        time.Now().UnixMilli(),
		})
	}
}

func telegramStart(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID string `json:"id"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.ID != "telegram" {
			return rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID).Response(req.ID)
		}
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return rpcerr.Unavailable("channel start failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "started")
		return rpcutil.RespondOK(req.ID, map[string]any{"started": true, "id": p.ID})
	}
}

func telegramStop(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID string `json:"id"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.ID != "telegram" {
			return rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID).Response(req.ID)
		}
		if err := deps.TelegramPlugin.Stop(ctx); err != nil {
			return rpcerr.Unavailable("channel stop failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelDisconnect, "stopped")
		return rpcutil.RespondOK(req.ID, map[string]any{"stopped": true, "id": p.ID})
	}
}

func telegramRestart(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID string `json:"id"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.ID != "telegram" {
			return rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID).Response(req.ID)
		}
		deps.TelegramPlugin.Stop(ctx)
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return rpcerr.Unavailable("channel restart failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "restarted")
		return rpcutil.RespondOK(req.ID, map[string]any{"restarted": true, "id": p.ID})
	}
}

// ---------------------------------------------------------------------------
// Messaging handlers
// ---------------------------------------------------------------------------

func messagingSend(deps MessagingDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
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
		}](req)
		if errResp != nil {
			return errResp
		}

		// Route to Telegram if channel matches and plugin is available.
		if (p.Channel == "" || p.Channel == "telegram") && deps.TelegramPlugin != nil {
			client := deps.TelegramPlugin.Client()
			if client == nil {
				return rpcerr.Unavailable("telegram client not connected").Response(req.ID)
			}

			chatID, err := parseChatID(p.To)
			if err != nil {
				return rpcerr.New(protocol.ErrInvalidRequest, "invalid chat ID: "+err.Error()).Response(req.ID)
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
				return rpcerr.New(protocol.ErrDependencyFailed, "telegram send failed: "+err.Error()).Response(req.ID)
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
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":      true,
				"channel": "telegram",
				"result":  resultData,
			})
		}

		// No other channels available in standalone Go gateway.
		return rpcerr.Unavailable("no channel available for sending").Response(req.ID)
	}
}

func messagingPoll(deps MessagingDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// If Telegram plugin is running, drain buffered messages.
		if deps.TelegramPlugin != nil {
			bot := deps.TelegramPlugin.Bot()
			if bot != nil {
				messages := bot.DrainMessages()
				return rpcutil.RespondOK(req.ID, map[string]any{
					"channel":  "telegram",
					"messages": messages,
					"count":    len(messages),
				})
			}
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"messages": []any{},
			"count":    0,
		})
	}
}

// parseChatID converts a string chat ID to int64 for the Telegram API.
func parseChatID(to string) (int64, error) {
	return telegram.ParseChatID(to)
}
