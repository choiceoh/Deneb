package server

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// registerAuthRPCMethods registers credential and channel-logout RPC methods.
// The Telegram bot was retired; telegram.logout is kept as a stub so existing
// clients that call it receive a clear "not found" instead of a crash.
func (s *Server) registerAuthRPCMethods() {
	s.dispatcher.Register("telegram.logout", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcerr.Newf(protocol.ErrNotFound, "Telegram channel has been retired").Response(req.ID)
	})
}
