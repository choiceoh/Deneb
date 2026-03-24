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
type Dispatcher struct {
	mu        sync.RWMutex
	handlers  map[string]HandlerFunc
	forwarder Forwarder
	logger    *slog.Logger
}

// NewDispatcher creates an empty RPC dispatcher.
func NewDispatcher(logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]HandlerFunc),
		logger:   logger,
	}
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
		return handler(ctx, req)
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
