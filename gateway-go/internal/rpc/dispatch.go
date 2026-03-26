// Package rpc handles RPC method dispatch for the gateway.
//
// Each domain (chat, sessions, agents, config, system, etc.) registers
// its method handlers here. This mirrors the structure of
// src/gateway/server-methods/ in the TypeScript codebase.
package rpc

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc processes an RPC request and returns a response.
type HandlerFunc func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame

// MiddlewareFunc wraps an RPC handler to add cross-cutting behavior (logging,
// rate-limiting, etc.). This type-aliases the middleware package's Middleware
// so callers don't need a separate import.
type MiddlewareFunc = middleware.Middleware

// Dispatcher routes RPC method calls to registered handlers.
// Optionally backed by a WorkerPool to bound concurrent handler goroutines
// and a middleware chain for cross-cutting concerns.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
	logger   *slog.Logger
	pool     *WorkerPool
	mw       middleware.Middleware // composed middleware chain (may be nil)
}

// NewDispatcher creates an empty RPC dispatcher with a default worker pool
// to prevent unbounded goroutine creation under burst load.
func NewDispatcher(logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]HandlerFunc),
		logger:   logger,
		pool:     NewWorkerPool(0), // default: 2× CPU cores, clamped [4, 64]
	}
}

// UseMiddleware installs a composed middleware chain that wraps every handler
// invocation. Middleware are applied in order: first middleware is outermost.
// Must be called before the first Dispatch; not safe for concurrent use.
func (d *Dispatcher) UseMiddleware(mws ...middleware.Middleware) {
	if len(mws) == 0 {
		return
	}
	d.mw = middleware.Chain(mws...)
}

// SetWorkerPool attaches a bounded worker pool for handler execution.
// When set, Dispatch routes handler calls through the pool instead of
// spawning unbounded goroutines. This prevents goroutine explosion
// under burst load.
func (d *Dispatcher) SetWorkerPool(pool *WorkerPool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pool = pool
}

// Register adds a handler for the given method name.
func (d *Dispatcher) Register(method string, handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[method] = handler
}

// Methods returns all registered method names.
func (d *Dispatcher) Methods() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	methods := make([]string, 0, len(d.handlers))
	for m := range d.handlers {
		methods = append(methods, m)
	}
	return methods
}

// Dispatch routes a request to the appropriate handler.
// Returns a NOT_FOUND error if no handler is registered for the method.
// If a middleware chain is installed via UseMiddleware, it wraps the handler.
func (d *Dispatcher) Dispatch(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	d.mu.RLock()
	handler, ok := d.handlers[req.Method]
	mw := d.mw
	d.mu.RUnlock()

	if !ok {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrNotFound,
			fmt.Sprintf("unknown method: %q", req.Method),
		))
	}

	// Wrap handler through middleware chain if one is installed.
	// Type conversion needed because rpc.HandlerFunc and middleware.HandlerFunc
	// are structurally identical but distinct named types.
	if mw != nil {
		wrapped := mw(middleware.HandlerFunc(handler))
		handler = HandlerFunc(wrapped)
	}

	return d.safeCall(ctx, req, handler)
}

// safeCall invokes a handler with panic recovery and a hard context deadline.
// If a WorkerPool is attached, the handler goroutine is submitted through
// the pool to bound concurrent handler execution. Panics are caught and
// converted to UNAVAILABLE error responses rather than crashing the server.
func (d *Dispatcher) safeCall(ctx context.Context, req *protocol.RequestFrame, handler HandlerFunc) *protocol.ResponseFrame {
	type result struct {
		resp *protocol.ResponseFrame
	}
	ch := make(chan result, 1)

	run := func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("handler panic", "method", req.Method, "panic", r,
					"stack", string(debug.Stack()))
				ch <- result{resp: protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable,
					fmt.Sprintf("internal error handling %q", req.Method),
				))}
			}
		}()
		ch <- result{resp: handler(ctx, req)}
	}

	d.mu.RLock()
	pool := d.pool
	d.mu.RUnlock()

	if pool != nil {
		pool.Submit(run)
	} else {
		go run()
	}

	select {
	case r := <-ch:
		return r.resp
	case <-ctx.Done():
		d.logger.Warn("handler timeout", "method", req.Method)
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrAgentTimeout,
			fmt.Sprintf("handler %q did not complete within deadline", req.Method),
		))
	}
}
