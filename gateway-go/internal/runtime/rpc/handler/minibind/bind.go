// Package minibind provides authenticated parameter binding for native-client
// RPC handlers. Authentication always precedes decoding.
package minibind

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Identity returns the authenticated native-client identity, if present.
func Identity(ctx context.Context) *clientauth.Identity {
	return clientauth.FromContext(ctx)
}

// Require returns UNAUTHORIZED when ctx has no native-client identity.
func Require(ctx context.Context, reqID string) *protocol.ResponseFrame {
	if Identity(ctx) == nil {
		return rpcerr.New(protocol.ErrUnauthorized, "miniapp request missing client identity context").Response(reqID)
	}
	return nil
}

// Authenticated decorates next with native-client authentication. The guard
// runs before any handler-side parameter decoding or dependency lookup.
func Authenticated(next rpcutil.HandlerFunc) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := Require(ctx, req.ID); errResp != nil {
			return errResp
		}
		return next(ctx, req)
	}
}

// Bind authenticates, decodes required params, then invokes next.
func Bind[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return Authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[P](req)
		if errResp != nil {
			return errResp
		}
		return next(ctx, req, p)
	})
}

// BindOptional is Bind for handlers whose empty params equal an empty object.
func BindOptional[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return Authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p P
		if len(req.Params) > 0 {
			var errResp *protocol.ResponseFrame
			p, errResp = rpcutil.DecodeParams[P](req)
			if errResp != nil {
				return errResp
			}
		}
		return next(ctx, req, p)
	})
}
