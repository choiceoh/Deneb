// Package middleware provides a request processing pipeline for the RPC dispatcher.
//
// Middleware functions wrap RPC handlers to add cross-cutting concerns like
// authentication, rate limiting, and logging. This mirrors the middleware pattern
// used in src/gateway/server.impl.ts for request processing.
package middleware

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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

// RequestContext carries per-request metadata through the middleware chain.
type RequestContext struct {
	ConnID        string
	Role          string
	Authenticated bool
	DeviceID      string
}

type contextKey string

const reqCtxKey contextKey = "reqCtx"

// WithRequestContext attaches request metadata to a context.
func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, reqCtxKey, rc)
}

// GetRequestContext retrieves request metadata from a context.
func GetRequestContext(ctx context.Context) *RequestContext {
	rc, _ := ctx.Value(reqCtxKey).(*RequestContext)
	return rc
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

// Auth returns a middleware that checks authentication for non-public methods.
func Auth(publicMethods map[string]bool) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if publicMethods[req.Method] {
				return next(ctx, req)
			}

			rc := GetRequestContext(ctx)
			if rc == nil || !rc.Authenticated {
				return rpcerr.Unauthorized("authentication required").Response(req.ID)
			}
			return next(ctx, req)
		}
	}
}

// RateLimitConfig configures the fixed-window rate limiter.
type RateLimitConfig struct {
	MaxRequests int
	WindowMs    int64
}

// RateLimiter is a stoppable rate-limiting middleware.
type RateLimiter struct {
	windows sync.Map
	cfg     RateLimitConfig
	stopCh  chan struct{}
}

// NewRateLimiter creates a rate limiter with background GC.
// Call Close() on shutdown to stop the GC goroutine.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}

	gcInterval := time.Duration(cfg.WindowMs*2) * time.Millisecond
	if gcInterval < time.Second {
		gcInterval = time.Second
	}
	go rl.gcLoop(gcInterval)

	return rl
}

// Close stops the background GC goroutine.
func (rl *RateLimiter) Close() {
	select {
	case <-rl.stopCh:
		// already closed
	default:
		close(rl.stopCh)
	}
}

func (rl *RateLimiter) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			cutoff := time.Now().UnixMilli() - rl.cfg.WindowMs*2
			rl.windows.Range(func(key, val any) bool {
				w := val.(*rateLimitWindow)
				w.mu.Lock()
				stale := w.startMs < cutoff
				w.mu.Unlock()
				if stale {
					rl.windows.Delete(key)
				}
				return true
			})
		}
	}
}

type rateLimitWindow struct {
	mu      sync.Mutex
	count   int
	startMs int64
}

// Middleware returns the rate-limiting middleware function.
func (rl *RateLimiter) Middleware() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			rc := GetRequestContext(ctx)
			if rc == nil {
				return next(ctx, req)
			}

			nowMs := time.Now().UnixMilli()
			key := rc.ConnID

			val, _ := rl.windows.LoadOrStore(key, &rateLimitWindow{startMs: nowMs})
			w := val.(*rateLimitWindow)

			w.mu.Lock()
			if nowMs-w.startMs >= rl.cfg.WindowMs {
				w.startMs = nowMs
				w.count = 0
			}
			w.count++
			count := w.count
			remaining := rl.cfg.WindowMs - (nowMs - w.startMs)
			w.mu.Unlock()

			if count > rl.cfg.MaxRequests {
				retryMs := uint64(remaining)
				retryable := true
				return protocol.NewResponseError(req.ID, &protocol.ErrorShape{
					Code:         "RATE_LIMITED",
					Message:      "too many requests",
					Retryable:    &retryable,
					RetryAfterMs: &retryMs,
				})
			}

			return next(ctx, req)
		}
	}
}
