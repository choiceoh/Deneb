package handlerminiapp

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PushTokenStore is the device-token registry the push RPCs write to. The
// concrete store lives in internal/domain/push; the interface keeps this
// handler decoupled from it (and trivially mockable).
type PushTokenStore interface {
	Register(token, platform string) (int, error)
	Unregister(token string) (int, error)
}

// PushDeps wires the push registration methods.
type PushDeps struct {
	Store PushTokenStore
}

// maxPushTokenLen caps a registration ID to a sane bound. FCM tokens are ~160+
// chars; this leaves generous headroom while rejecting absurd input.
const maxPushTokenLen = 4096

// PushMethods returns the miniapp.push.* handler map. The native client
// registers its FCM registration ID here so the gateway can deliver proactive
// notifications when no live SSE connection is held (app fully closed / Doze).
// Returns nil (methods unregistered) when no store is wired.
func PushMethods(deps PushDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.push.register":   pushRegister(deps),
		"miniapp.push.unregister": pushUnregister(deps),
	}
}

func pushRegister(deps PushDeps) rpcutil.HandlerFunc {
	type params struct {
		Token    string `json:"token"`
		Platform string `json:"platform,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.Token == "" {
			return rpcerr.MissingParam("token").Response(req.ID)
		}
		if len(p.Token) > maxPushTokenLen {
			return rpcerr.New(protocol.ErrInvalidRequest, "token too long").Response(req.ID)
		}
		count, err := deps.Store.Register(p.Token, p.Platform)
		if err != nil {
			return rpcerr.WrapUnavailable("push token registration failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "count": count})
	}
}

func pushUnregister(deps PushDeps) rpcutil.HandlerFunc {
	type params struct {
		Token string `json:"token"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.Token == "" {
			return rpcerr.MissingParam("token").Response(req.ID)
		}
		count, err := deps.Store.Unregister(p.Token)
		if err != nil {
			return rpcerr.WrapUnavailable("push token unregister failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "count": count})
	}
}
