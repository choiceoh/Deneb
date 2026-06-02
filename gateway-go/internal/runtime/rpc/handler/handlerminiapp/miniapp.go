// Package handlerminiapp implements RPC handlers for the Telegram Mini App
// surface (miniapp.* methods). Every method assumes the request has already
// passed initData verification, so the handlers can pull the authenticated
// Telegram user straight from the context via telegram.InitDataFromContext.
//
// The current method set is intentionally minimal — just enough to prove the
// HTTP → middleware → dispatcher → handler path end-to-end. Real domain
// methods (Gmail triage, memory search, etc.) live in follow-up packages.
package handlerminiapp

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps groups the values miniapp handlers need at registration time.
type Deps struct {
	// Version is the gateway build version reported back by miniapp.ping so the
	// client can show "Backend: 4.22.3 (12ms)" without an extra RPC round-trip.
	Version string
	// CurrentModel resolves the model ID currently in effect for new runs.
	// Called lazily at request time because chatHandler / modelRegistry are
	// created after early-phase registration. May be nil; returns "" when
	// no model is resolvable.
	CurrentModel func() string
}

// Methods returns the miniapp.* handler map. All methods require initData
// verification — the calling middleware must have stored the verified
// *telegram.InitData on the context.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.ping":   ping(deps),
		"miniapp.whoami": whoami(),
	}
}

// ping returns liveness info. Useful for the client to render a heartbeat
// without waiting on a domain query.
func ping(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.ping requires initData context").Response(req.ID)
		}
		payload := map[string]any{
			"ok":      true,
			"version": deps.Version,
			"tsMs":    time.Now().UnixMilli(),
		}
		if deps.CurrentModel != nil {
			if m := deps.CurrentModel(); m != "" {
				payload["model"] = m
			}
		}
		return rpcutil.RespondOK(req.ID, payload)
	}
}

// whoami echoes back the Telegram user the middleware authenticated. The
// client uses this to render "Hello, <firstName>" and confirm that initData
// verification is intact.
func whoami() rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		data := clientauth.FromContext(ctx)
		if data == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.whoami requires initData context").Response(req.ID)
		}
		if data.User == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "initData missing user (channel-bot launch?)").Response(req.ID)
		}
		u := data.User
		return rpcutil.RespondOK(req.ID, map[string]any{
			"id":           u.ID,
			"firstName":    u.FirstName,
			"lastName":     u.LastName,
			"username":     u.Username,
			"languageCode": u.LanguageCode,
			"isPremium":    u.IsPremium,
			"authDateMs":   data.AuthDate.UnixMilli(),
		})
	}
}
