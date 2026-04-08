// Package handlertelegram provides RPC handlers for Telegram lifecycle
// (start/stop/restart) and messaging (send/poll) methods.
package handlertelegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
			defer func() { recover() }() //nolint:errcheck // fire-and-forget panic recovery
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
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel start failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "started")
		return map[string]any{"started": true, "id": p.ID}, nil
	})
}

func telegramStop(deps LifecycleDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		if err := deps.TelegramPlugin.Stop(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel stop failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelDisconnect, "stopped")
		return map[string]any{"stopped": true, "id": p.ID}, nil
	})
}

func telegramRestart(deps LifecycleDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		deps.TelegramPlugin.Stop(ctx) //nolint:errcheck // best-effort cleanup before restart
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel restart failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "restarted")
		return map[string]any{"restarted": true, "id": p.ID}, nil
	})
}

// ---------------------------------------------------------------------------
// Messaging handlers
// ---------------------------------------------------------------------------

func messagingSend(deps MessagingDeps) rpcutil.HandlerFunc {
	type mediaEntry struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	type params struct {
		Channel  string       `json:"channel"`
		To       string       `json:"to"`
		Text     string       `json:"text"`
		Media    []mediaEntry `json:"media,omitempty"`
		ReplyTo  int64        `json:"replyTo,omitempty"`
		ThreadID int64        `json:"threadId,omitempty"`
		Silent   bool         `json:"silent,omitempty"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if (p.Channel == "" || p.Channel == "telegram") && deps.TelegramPlugin != nil {
			client := deps.TelegramPlugin.Client()
			if client == nil {
				return nil, rpcerr.Unavailable("telegram client not connected")
			}
			chatID, err := parseChatID(p.To)
			if err != nil {
				return nil, rpcerr.WrapInvalidRequest("invalid chat ID", err)
			}
			opts := telegram.SendOptions{
				ParseMode:           "HTML",
				ThreadID:            p.ThreadID,
				DisableNotification: p.Silent,
				ReplyToMessageID:    p.ReplyTo,
			}
			html := telegram.FormatHTML(p.Text)
			results, err := telegram.SendText(ctx, client, chatID, html, opts)
			if err != nil {
				return nil, rpcerr.WrapDependencyFailed("telegram send failed", err)
			}

			// Send media attachments, collecting errors.
			var mediaErrors []error
			for _, m := range p.Media {
				var sendErr error
				switch m.Type {
				case "photo", "image":
					_, sendErr = telegram.SendPhoto(ctx, client, chatID, m.URL, "", opts)
				case "document", "file":
					_, sendErr = telegram.SendDocument(ctx, client, chatID, m.URL, "", opts)
				case "video":
					_, sendErr = telegram.SendVideo(ctx, client, chatID, m.URL, "", opts)
				case "audio":
					_, sendErr = telegram.SendAudio(ctx, client, chatID, m.URL, "", opts)
				case "voice":
					_, sendErr = telegram.SendVoice(ctx, client, chatID, m.URL, "", opts)
				}
				if sendErr != nil {
					mediaErrors = append(mediaErrors, fmt.Errorf("media %s: %w", m.Type, sendErr))
				}
			}
			if err := errors.Join(mediaErrors...); err != nil {
				slog.Warn("telegram media send errors", "count", len(mediaErrors), "error", err)
			}
			var resultData any
			if len(results) > 0 {
				resultData = results[0]
			}
			return map[string]any{
				"ok":      true,
				"channel": "telegram",
				"result":  resultData,
			}, nil
		}
		return nil, rpcerr.Unavailable("no channel available for sending")
	})
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
