package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
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
	// DeliveryEnabled reports whether the FCM handoff is currently usable
	// (credentials loaded and its OAuth dependency reachable). Late-bound closure:
	// evaluated per request so wiring order against the notifier setup does not
	// matter. The native client uses this to decide whether background-Doze mode
	// is safe (battery doc section 3.1). Nil means unknown, reported as false.
	DeliveryEnabled func(context.Context) bool
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
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
		delivery := deps.DeliveryEnabled != nil && deps.DeliveryEnabled(ctx)
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "count": count, "delivery": delivery})
	})
}

func pushUnregister(deps PushDeps) rpcutil.HandlerFunc {
	type params struct {
		Token string `json:"token"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if p.Token == "" {
			return rpcerr.MissingParam("token").Response(req.ID)
		}
		count, err := deps.Store.Unregister(p.Token)
		if err != nil {
			return rpcerr.WrapUnavailable("push token unregister failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "count": count})
	})
}
