package metrics

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/middleware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RPCInstrumentation returns a middleware that counts every RPC call
// in RPCRequestsTotal (method × status × error code).
func RPCInstrumentation() middleware.Middleware {
	return func(next middleware.HandlerFunc) middleware.HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			resp := next(ctx, req)

			status := "ok"
			code := ""
			if !resp.OK {
				status = "error"
				if resp.Error != nil {
					code = resp.Error.Code
				}
			}

			RPCRequestsTotal.Inc(req.Method, status, code)
			return resp
		}
	}
}
