package metrics

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/middleware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RPCInstrumentation returns a middleware that records metrics for every
// RPC call: request count (by method+status+code) and duration histogram.
func RPCInstrumentation() middleware.Middleware {
	return func(next middleware.HandlerFunc) middleware.HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			start := time.Now()
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
			RPCDuration.ObserveDuration(start, req.Method)

			return resp
		}
	}
}
