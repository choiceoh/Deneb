package metrics

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RPCInstrumentation returns a middleware that records metrics for every
// RPC call: request count (by method+status) and duration histogram.
func RPCInstrumentation() middleware.Middleware {
	return func(next middleware.HandlerFunc) middleware.HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			start := time.Now()
			resp := next(ctx, req)

			status := "ok"
			if !resp.OK {
				status = "error"
			}

			RPCRequestsTotal.Inc(req.Method, status)
			RPCDuration.ObserveDuration(start, req.Method)

			return resp
		}
	}
}
