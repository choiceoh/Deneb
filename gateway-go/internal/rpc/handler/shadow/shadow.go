// Package shadow provides RPC handlers for the shadow monitoring service.
package shadow

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	shadowsvc "github.com/choiceoh/deneb/gateway-go/internal/shadow"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for shadow RPC methods.
type Deps struct {
	Shadow *shadowsvc.Service
}

// Methods returns all shadow-related RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Shadow == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"shadow.status": handleStatus(deps),
	}
}

func handleStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Shadow.Status())
	}
}
