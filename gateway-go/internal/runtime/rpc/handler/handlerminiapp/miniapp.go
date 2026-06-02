// Package handlerminiapp implements RPC handlers for the native-client surface
// (miniapp.* methods). Every method assumes the request has already passed
// client-token verification, so handlers can pull the authenticated operator
// identity straight from context via clientauth.FromContext.
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
	// Capabilities resolves the native-client feature flags currently available
	// on this gateway. Called lazily because several subsystems are wired after
	// early miniapp method registration.
	Capabilities func() map[string]bool
}

// Methods returns the miniapp.* handler map. All methods require client-token
// verification — the HTTP bridge must have stored a clientauth.Identity on the
// context before dispatch.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.ping":         ping(deps),
		"miniapp.whoami":       whoami(),
		"miniapp.client.hello": clientHello(deps),
	}
}

// ping returns liveness info. Useful for the client to render a heartbeat
// without waiting on a domain query.
func ping(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.ping requires client identity context").Response(req.ID)
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

// clientHello returns the native-client contract snapshot in one cheap call:
// version, current model, known endpoint paths, and feature flags. The Android
// app uses this to show accurate gateway status and gate native-only surfaces
// without probing multiple domain RPCs.
func clientHello(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.client.hello requires client identity context").Response(req.ID)
		}
		caps := map[string]bool{"rpc": true}
		if deps.Capabilities != nil {
			for k, v := range deps.Capabilities() {
				caps[k] = v
			}
		}
		payload := map[string]any{
			"ok":               true,
			"version":          deps.Version,
			"nativeApiVersion": 1,
			"tsMs":             time.Now().UnixMilli(),
			"capabilities":     caps,
			"endpoints": map[string]string{
				"rpc":        "/api/v1/miniapp/rpc",
				"chatStream": "/api/v1/miniapp/chat/stream",
				"events":     "/api/v1/miniapp/events",
			},
		}
		if deps.CurrentModel != nil {
			if m := deps.CurrentModel(); m != "" {
				payload["model"] = m
			}
		}
		return rpcutil.RespondOK(req.ID, payload)
	}
}

// whoami echoes back the native operator identity the middleware authenticated.
// The client uses this to confirm that client-token verification is intact.
func whoami() rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		data := clientauth.FromContext(ctx)
		if data == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.whoami requires client identity context").Response(req.ID)
		}
		if data.User == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "client identity missing user").Response(req.ID)
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
