// Package handlerevents provides RPC handlers for session event subscription
// and broadcast methods. These are transport-agnostic (not Telegram-specific).
package handlerevents

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
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

	// Define handlers once, register under both legacy and TS-compatible names.
	subscribeSession := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConnID string `json:"connId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ConnID == "" {
			return rpcerr.MissingParam("connId").Response(req.ID)
		}
		deps.Broadcaster.SubscribeSessionEvents(p.ConnID)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeSession := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConnID string `json:"connId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ConnID == "" {
			return rpcerr.MissingParam("connId").Response(req.ID)
		}
		deps.Broadcaster.UnsubscribeSessionEvents(p.ConnID)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

	subscribeMessages := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ConnID == "" || p.SessionKey == "" {
			return rpcerr.MissingParam("connId and sessionKey").Response(req.ID)
		}
		deps.Broadcaster.SubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeMessages := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ConnID == "" || p.SessionKey == "" {
			return rpcerr.MissingParam("connId and sessionKey").Response(req.ID)
		}
		deps.Broadcaster.UnsubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

	// Tool event subscription: routes session.tool events for a specific run
	// to a single connection instead of broadcasting to all subscribers.
	subscribeToolEvents := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConnID string `json:"connId"`
			RunID  string `json:"runId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ConnID == "" || p.RunID == "" {
			return rpcerr.MissingParam("connId and runId").Response(req.ID)
		}
		deps.Broadcaster.RegisterToolEventRecipient(p.RunID, p.ConnID)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	}

	unsubscribeToolEvents := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			RunID string `json:"runId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.RunID == "" {
			return rpcerr.MissingParam("runId").Response(req.ID)
		}
		deps.Broadcaster.UnregisterToolEventRecipient(p.RunID)
		resp := rpcutil.RespondOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	}

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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Event == "" {
			return rpcerr.MissingParam("event").Response(req.ID)
		}
		sent, _ := deps.Broadcaster.Broadcast(p.Event, p.Payload)
		return rpcutil.RespondOK(req.ID, map[string]int{"sent": sent})
	}
}
