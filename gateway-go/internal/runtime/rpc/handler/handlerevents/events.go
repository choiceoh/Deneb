// Package handlerevents provides RPC handlers for session event subscription
// and broadcast methods. These are transport-agnostic (not Telegram-specific).
package handlerevents

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// EventsDeps holds the dependencies for event subscription RPC methods
// (subscribe.session, sessions.subscribe, etc.).
type EventsDeps struct {
	Broadcaster *events.Broadcaster
	Logger      *slog.Logger
}

// EventsMethods returns event subscription and streaming RPC handlers.
// Also includes TS-compatible aliases (sessions.subscribe, etc.)
// that map to the same handlers as subscribe.session, etc.
// Returns nil if Broadcaster is not configured.
//
// Note: "node.event" is registered by handlernode.Methods (not here) to
// avoid duplicate registration — the node package's implementation is the
// authoritative handler for node event broadcasting.
func EventsMethods(deps EventsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}

	type connParams struct {
		ConnID string `json:"connId"`
	}
	type connSessionParams struct {
		ConnID     string `json:"connId"`
		SessionKey string `json:"sessionKey"`
	}
	type connRunParams struct {
		ConnID string `json:"connId"`
		RunID  string `json:"runId"`
	}
	type runParams struct {
		RunID string `json:"runId"`
	}

	subscribeSession := rpcutil.BindHandler[connParams](func(p connParams) (any, error) {
		if p.ConnID == "" {
			return nil, rpcerr.MissingParam("connId")
		}
		deps.Broadcaster.SubscribeSessionEvents(p.ConnID)
		return map[string]bool{"subscribed": true}, nil
	})

	unsubscribeSession := rpcutil.BindHandler[connParams](func(p connParams) (any, error) {
		if p.ConnID == "" {
			return nil, rpcerr.MissingParam("connId")
		}
		deps.Broadcaster.UnsubscribeSessionEvents(p.ConnID)
		return map[string]bool{"unsubscribed": true}, nil
	})

	subscribeMessages := rpcutil.BindHandler[connSessionParams](func(p connSessionParams) (any, error) {
		if p.ConnID == "" || p.SessionKey == "" {
			return nil, rpcerr.MissingParam("connId and sessionKey")
		}
		deps.Broadcaster.SubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		return map[string]bool{"subscribed": true}, nil
	})

	unsubscribeMessages := rpcutil.BindHandler[connSessionParams](func(p connSessionParams) (any, error) {
		if p.ConnID == "" || p.SessionKey == "" {
			return nil, rpcerr.MissingParam("connId and sessionKey")
		}
		deps.Broadcaster.UnsubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		return map[string]bool{"unsubscribed": true}, nil
	})

	// Tool event subscription: routes session.tool events for a specific run
	// to a single connection instead of broadcasting to all subscribers.
	subscribeToolEvents := rpcutil.BindHandler[connRunParams](func(p connRunParams) (any, error) {
		if p.ConnID == "" || p.RunID == "" {
			return nil, rpcerr.MissingParam("connId and runId")
		}
		deps.Broadcaster.RegisterToolEventRecipient(p.RunID, p.ConnID)
		return map[string]bool{"subscribed": true}, nil
	})

	unsubscribeToolEvents := rpcutil.BindHandler[runParams](func(p runParams) (any, error) {
		if p.RunID == "" {
			return nil, rpcerr.MissingParam("runId")
		}
		deps.Broadcaster.UnregisterToolEventRecipient(p.RunID)
		return map[string]bool{"unsubscribed": true}, nil
	})

	return map[string]rpcutil.HandlerFunc{
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

// BroadcastMethods returns the events.broadcast handler.
func BroadcastMethods(deps EventsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"events.broadcast": eventsBroadcast(deps),
	}
}

func eventsBroadcast(deps EventsDeps) rpcutil.HandlerFunc {
	type params struct {
		Event   string `json:"event"`
		Payload any    `json:"payload"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Event == "" {
			return nil, rpcerr.MissingParam("event")
		}
		sent, _ := deps.Broadcaster.Broadcast(p.Event, p.Payload)
		return map[string]int{"sent": sent}, nil
	})
}
