// Package middleware provides a request processing pipeline for the RPC dispatcher.
package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc processes an RPC request and returns a response.
type HandlerFunc func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame

// Middleware wraps a HandlerFunc to add pre/post processing.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain composes multiple middleware into a single middleware.
// Middleware are applied in order: first middleware is outermost (runs first).
func Chain(middlewares ...Middleware) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// Logging returns a middleware that logs RPC request/response timing.
func Logging(logger *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			start := time.Now()
			resp := next(ctx, req)
			elapsed := time.Since(start)

			level := slog.LevelDebug
			if elapsed > 5*time.Second {
				level = slog.LevelWarn
			}

			logger.Log(ctx, level, "rpc",
				"method", req.Method,
				"id", req.ID,
				"ok", resp.OK,
				"ms", elapsed.Milliseconds(),
			)
			return resp
		}
	}
}
