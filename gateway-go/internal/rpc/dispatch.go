// Package rpc handles RPC method dispatch for the gateway.
//
// Each domain (chat, sessions, agents, config, system, etc.) registers
// its method handlers here. This mirrors the structure of
// src/gateway/server-methods/ in the TypeScript codebase.
package rpc

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Handler processes an RPC request and returns a result or error.
type Handler func(params json.RawMessage) (any, error)

// Dispatcher routes RPC method calls to registered handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewDispatcher creates an empty RPC dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler for the given method name.
func (d *Dispatcher) Register(method string, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[method] = handler
}

// Dispatch routes a method call to its handler.
func (d *Dispatcher) Dispatch(method string, params json.RawMessage) (any, error) {
	d.mu.RLock()
	handler, ok := d.handlers[method]
	d.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}
	return handler(params)
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
