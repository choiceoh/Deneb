// Package channel provides RPC handlers for channel lifecycle, event
// subscription, and messaging (send/poll) methods. These were migrated from
// the flat rpc package (methods_channel.go, methods_events.go,
// methods_messaging.go) into a domain-based handler subpackage.
package channel

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	channelpkg "github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Deps structs
// ---------------------------------------------------------------------------

// LifecycleDeps holds the dependencies for channel lifecycle RPC methods
// (channels.start, channels.stop, channels.restart).
type LifecycleDeps struct {
	ChannelLifecycle *channelpkg.LifecycleManager
	Hooks            *hooks.Registry
	Broadcaster      *events.Broadcaster
}

// EventsDeps holds the dependencies for event subscription RPC methods
// (subscribe.session, sessions.subscribe, node.event, etc.).
type EventsDeps struct {
	Broadcaster *events.Broadcaster
	Logger      *slog.Logger
}

// MessagingDeps holds dependencies for send/poll RPC methods.
type MessagingDeps struct {
	// TelegramPlugin is the native Telegram channel plugin (nil if not configured).
	TelegramPlugin *telegram.Plugin
}

// ---------------------------------------------------------------------------
// Method registries
// ---------------------------------------------------------------------------

// LifecycleMethods returns channel start/stop/restart RPC handlers.
// Returns nil if ChannelLifecycle is not configured.
func LifecycleMethods(deps LifecycleDeps) map[string]rpcutil.HandlerFunc {
	if deps.ChannelLifecycle == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"channels.start":   channelStart(deps),
		"channels.stop":    channelStop(deps),
		"channels.restart": channelRestart(deps),
	}
}

// EventsMethods returns event subscription, streaming, and node event RPC
// handlers. Also includes TS-compatible aliases (sessions.subscribe, etc.)
// that map to the same handlers as subscribe.session, etc.
// Returns nil if Broadcaster is not configured.
func EventsMethods(deps EventsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}

	// Node event relay: processes events from connected nodes.
	nodeEvent := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID string           `json:"nodeId"`
			Event  events.NodeEvent `json:"event"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.NodeID == "" {
			return rpcerr.MissingParam("nodeId and event").Response(req.ID)
		}
		nodeCtx := &events.NodeEventContext{
			Broadcaster: deps.Broadcaster,
			Logger:      deps.Logger,
		}
		events.HandleNodeEvent(nodeCtx, p.NodeID, p.Event)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}

	// Define handlers once, register under both legacy and TS-compatible names.
	subscribeSession := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID string `json:"connId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" {
			return rpcerr.MissingParam("connId").Response(req.ID)
		}
		deps.Broadcaster.SubscribeSessionEvents(p.ConnID)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeSession := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID string `json:"connId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" {
			return rpcerr.MissingParam("connId").Response(req.ID)
		}
		deps.Broadcaster.UnsubscribeSessionEvents(p.ConnID)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

	subscribeMessages := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" || p.SessionKey == "" {
			return rpcerr.MissingParam("connId and sessionKey").Response(req.ID)
		}
		deps.Broadcaster.SubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeMessages := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" || p.SessionKey == "" {
			return rpcerr.MissingParam("connId and sessionKey").Response(req.ID)
		}
		deps.Broadcaster.UnsubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

	// Tool event subscription: routes session.tool events for a specific run
	// to a single connection instead of broadcasting to all subscribers.
	subscribeToolEvents := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID string `json:"connId"`
			RunID  string `json:"runId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" || p.RunID == "" {
			return rpcerr.MissingParam("connId and runId").Response(req.ID)
		}
		deps.Broadcaster.RegisterToolEventRecipient(p.RunID, p.ConnID)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeToolEvents := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RunID string `json:"runId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RunID == "" {
			return rpcerr.MissingParam("runId").Response(req.ID)
		}
		deps.Broadcaster.UnregisterToolEventRecipient(p.RunID)
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

	return map[string]rpcutil.HandlerFunc{
		// Node event relay.
		"node.event": nodeEvent,

		// Legacy Go names.
		"subscribe.session":            subscribeSession,
		"unsubscribe.session":          unsubscribeSession,
		"subscribe.session.messages":   subscribeMessages,
		"unsubscribe.session.messages": unsubscribeMessages,

		// TS-compatible aliases.
		"sessions.subscribe":            subscribeSession,
		"sessions.unsubscribe":          unsubscribeSession,
		"sessions.messages.subscribe":   subscribeMessages,
		"sessions.messages.unsubscribe": unsubscribeMessages,

		// Tool event routing.
		"sessions.tools.subscribe":   subscribeToolEvents,
		"sessions.tools.unsubscribe": unsubscribeToolEvents,
	}
}

func eventsBroadcast(deps EventsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return rpcerr.MissingParam("event").Response(req.ID)
		}
		sent, _ := deps.Broadcaster.Broadcast(p.Event, p.Payload)
		return protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
	}
}

// BroadcastMethods returns the events.broadcast handler.
func BroadcastMethods(deps EventsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"events.broadcast": eventsBroadcast(deps),
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
// Channel lifecycle handlers
// ---------------------------------------------------------------------------

// emitChannelLifecycleEvent fires the appropriate hook and broadcasts a
// channels.changed event after a successful channel operation.
func emitChannelLifecycleEvent(deps LifecycleDeps, id string, hookEvent hooks.Event, action string) {
	if deps.Hooks != nil {
		go func() {
			defer func() { recover() }()
			deps.Hooks.Fire(context.Background(), hookEvent, map[string]string{
				"DENEB_CHANNEL_ID": id,
			})
		}()
	}
	if deps.Broadcaster != nil {
		deps.Broadcaster.Broadcast("channels.changed", map[string]any{
			"channelId": id,
			"action":    action,
			"ts":        time.Now().UnixMilli(),
		})
	}
}

func channelStart(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if err := deps.ChannelLifecycle.StartChannel(ctx, p.ID); err != nil {
			return rpcerr.Unavailable("channel start failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "started")
		resp := protocol.MustResponseOK(req.ID, map[string]any{"started": true, "id": p.ID})
		return resp
	}
}

func channelStop(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if err := deps.ChannelLifecycle.StopChannel(ctx, p.ID); err != nil {
			return rpcerr.Unavailable("channel stop failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelDisconnect, "stopped")
		resp := protocol.MustResponseOK(req.ID, map[string]any{"stopped": true, "id": p.ID})
		return resp
	}
}

func channelRestart(deps LifecycleDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if err := deps.ChannelLifecycle.RestartChannel(ctx, p.ID); err != nil {
			return rpcerr.Unavailable("channel restart failed: " + err.Error()).WithChannel(p.ID).Response(req.ID)
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "restarted")
		resp := protocol.MustResponseOK(req.ID, map[string]any{"restarted": true, "id": p.ID})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Messaging handlers
// ---------------------------------------------------------------------------

func messagingSend(deps MessagingDeps) rpcutil.HandlerFunc {
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
			return rpcerr.InvalidParams(err).Response(req.ID)
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
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ok":      true,
				"channel": "telegram",
				"result":  resultData,
			})
			return resp
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

// parseChatID converts a string chat ID to int64 for the Telegram API.
func parseChatID(to string) (int64, error) {
	return telegram.ParseChatID(to)
}
