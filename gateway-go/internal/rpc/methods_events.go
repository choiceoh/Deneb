package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// EventsDeps holds the dependencies for event subscription RPC methods.
type EventsDeps struct {
	Broadcaster *events.Broadcaster
}

// RegisterEventsMethods registers event subscription and streaming RPC methods.
func RegisterEventsMethods(d *Dispatcher, deps EventsDeps) {
	if deps.Broadcaster == nil {
		return
	}

	d.Register("subscribe.session", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID string `json:"connId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "connId is required"))
		}
		deps.Broadcaster.SubscribeSessionEvents(p.ConnID)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	})

	d.Register("unsubscribe.session", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID string `json:"connId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "connId is required"))
		}
		deps.Broadcaster.UnsubscribeSessionEvents(p.ConnID)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	})

	d.Register("subscribe.session.messages", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" || p.SessionKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "connId and sessionKey are required"))
		}
		deps.Broadcaster.SubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"subscribed": true})
		return resp
	})

	d.Register("unsubscribe.session.messages", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConnID     string `json:"connId"`
			SessionKey string `json:"sessionKey"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ConnID == "" || p.SessionKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "connId and sessionKey are required"))
		}
		deps.Broadcaster.UnsubscribeSessionMessageEvents(p.ConnID, p.SessionKey)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"unsubscribed": true})
		return resp
	})
}
