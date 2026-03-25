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

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc processes an RPC request and returns a response.
type HandlerFunc func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame

// Forwarder forwards unhandled RPC requests to an external process (e.g., Node.js Plugin Host).
type Forwarder interface {
	Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error)
}

// Dispatcher routes RPC method calls to registered handlers.
// Optionally backed by a WorkerPool to bound concurrent handler goroutines.
type Dispatcher struct {
	mu        sync.RWMutex
	handlers  map[string]HandlerFunc
	forwarder Forwarder
	logger    *slog.Logger
	pool      *WorkerPool
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

// SetForwarder sets the fallback forwarder for unhandled methods.
func (d *Dispatcher) SetForwarder(f Forwarder) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.forwarder = f
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
// If no handler is registered and a forwarder is set, the request is forwarded.
// Returns a NOT_FOUND error if no handler or forwarder can handle the method.
func (d *Dispatcher) Dispatch(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	d.mu.RLock()
	handler, ok := d.handlers[req.Method]
	forwarder := d.forwarder
	d.mu.RUnlock()

	if ok {
		return d.safeCall(ctx, req, handler)
	}

	// Forward to Plugin Host bridge if available.
	if forwarder != nil {
		resp, err := forwarder.Forward(ctx, req)
		if err != nil {
			d.logger.Error("bridge forward failed", "method", req.Method, "error", err)
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed,
				"plugin host bridge error: "+err.Error(),
			))
		}
		return resp
	}

	return protocol.NewResponseError(req.ID, protocol.NewError(
		protocol.ErrNotFound,
		fmt.Sprintf("unknown method: %q", req.Method),
	))
}

// safeCall invokes a handler with panic recovery and a hard deadline.
// If a WorkerPool is attached, the handler goroutine is submitted through
// the pool so concurrent handler execution is bounded.
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
