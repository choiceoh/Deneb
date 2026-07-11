package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// authenticated decorates a miniapp handler with native-client authentication.
// Authentication deliberately runs before parameter decoding so an unauthenticated
// malformed request still returns UNAUTHORIZED rather than INVALID_REQUEST.
func authenticated(next rpcutil.HandlerFunc) rpcutil.HandlerFunc {
	return minibind.Authenticated(next)
}

// bindAuthenticated authenticates, decodes required params, then invokes next.
func bindAuthenticated[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.Bind(next)
}

// bindAuthenticatedOptional is bindAuthenticated for handlers whose empty
// params are valid and equivalent to an empty object.
func bindAuthenticatedOptional[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.BindOptional(next)
}
