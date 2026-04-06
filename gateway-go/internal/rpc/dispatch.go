// Package rpc handles RPC method dispatch for the gateway.
//
// Each domain (chat, sessions, agents, config, system, etc.) registers
// its method handlers here. This mirrors the structure of
// src/gateway/server-methods/ in the TypeScript codebase.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc is an alias for rpcutil.HandlerFunc so domain handler packages
// and the dispatcher share a single type — no conversion needed.
type HandlerFunc = rpcutil.HandlerFunc

// MiddlewareFunc wraps an RPC handler to add cross-cutting behavior (logging,
// rate-limiting, etc.). This type-aliases the middleware package's Middleware
// so callers don't need a separate import.
type MiddlewareFunc = middleware.Middleware

// handlerSnapshot is an immutable map stored in atomic.Value.
// After boot-time registration completes, the snapshot is published once
// and never mutated, so Dispatch reads it without any locking.
type handlerSnapshot map[string]HandlerFunc

// Dispatcher routes RPC method calls to registered handlers.
// Optionally backed by a WorkerPool to bound concurrent handler goroutines
// and a middleware chain for cross-cutting concerns.
//
// The handler map is stored as an atomic immutable snapshot (handlerSnapshot)
// so the hot dispatch path is lock-free. Registration (boot-time only) uses
// a sync.Mutex to serialize writes and publish new snapshots.
type Dispatcher struct {
	// snap holds the current handlerSnapshot. Loaded atomically on every
	// Dispatch call — zero contention on the hot path.
	snap atomic.Value // handlerSnapshot

	mu sync.Mutex // protects registration-time mutable state below
	// staging is the mutable map used during boot-time registration.
	// After each Register call the map is published to snap.
	staging map[string]HandlerFunc

	logger *slog.Logger
	pool   atomic.Pointer[WorkerPool]
	mw     middleware.Middleware // composed middleware chain (may be nil)

	registryValidation *registryValidationState
}

type registryValidationState struct {
	module string
	errs   []error
}

// NewDispatcher creates an empty RPC dispatcher with a default worker pool
// to prevent unbounded goroutine creation under burst load.
func NewDispatcher(logger *slog.Logger) *Dispatcher {
	d := &Dispatcher{
		staging: make(map[string]HandlerFunc),
		logger:  logger,
	}
	d.snap.Store(handlerSnapshot{})
	pool := NewWorkerPool(0) // default: 2× CPU cores, clamped [4, 64]
	d.pool.Store(pool)
	return d
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
	d.pool.Store(pool)
}

// Register adds a handler for the given method name.
// Must be called during boot; not safe for concurrent use with Dispatch.
func (d *Dispatcher) Register(method string, handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.registryValidation != nil {
		if _, exists := d.staging[method]; exists {
			module := d.registryValidation.module
			if module == "" {
				module = "unknown"
			}
			d.registryValidation.errs = append(d.registryValidation.errs,
				fmt.Errorf("duplicate rpc method %q in module %q", method, module))
			return
		}
	}
	d.staging[method] = handler
	d.publishLocked()
}

// publishLocked copies the staging map into an immutable snapshot and stores
// it in the atomic.Value. Must be called with d.mu held.
func (d *Dispatcher) publishLocked() {
	snap := make(handlerSnapshot, len(d.staging))
	for k, v := range d.staging {
		snap[k] = v
	}
	d.snap.Store(snap)
}

func (d *Dispatcher) beginRegistryValidation() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.registryValidation = &registryValidationState{}
}

func (d *Dispatcher) setRegistryModule(module string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.registryValidation != nil {
		d.registryValidation.module = module
	}
}

func (d *Dispatcher) endRegistryValidation() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.registryValidation == nil {
		return nil
	}
	err := errors.Join(d.registryValidation.errs...)
	d.registryValidation = nil
	return err
}

// PoolStats returns a snapshot of worker pool utilization, or a zero
// value if no pool is attached.
func (d *Dispatcher) PoolStats() WorkerPoolStats {
	if pool := d.pool.Load(); pool != nil {
		return pool.Stats()
	}
	return WorkerPoolStats{}
}

// Methods returns all registered method names.
func (d *Dispatcher) Methods() []string {
	snap := d.snap.Load().(handlerSnapshot)
	methods := make([]string, 0, len(snap))
	for m := range snap {
		methods = append(methods, m)
	}
	return methods
}

// Dispatch routes a request to the appropriate handler.
// Returns a NOT_FOUND error if no handler is registered for the method.
// If a middleware chain is installed via UseMiddleware, it wraps the handler.
//
// The handler lookup is lock-free: it loads an immutable snapshot from
// atomic.Value, avoiding RWMutex contention on every RPC call.
func (d *Dispatcher) Dispatch(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	snap := d.snap.Load().(handlerSnapshot)
	handler, ok := snap[req.Method]

	if !ok {
		return rpcerr.Newf(protocol.ErrNotFound, "unknown method: %q", req.Method).Response(req.ID)
	}

	// Wrap handler through middleware chain if one is installed.
	// HandlerFunc is an alias for middleware.HandlerFunc, so no conversion needed.
	if mw := d.mw; mw != nil {
		handler = mw(handler)
	}

	return d.safeCall(ctx, req, handler)
}

// safeCall invokes a handler with panic recovery and a hard context deadline.
// If a WorkerPool is attached, the handler goroutine is submitted through
// the pool to bound concurrent handler execution. Panics are caught and
// converted to UNAVAILABLE error responses rather than crashing the server.
//
// When the caller's context expires before the handler finishes, safeCall
// cancels a derived context so the handler can observe the cancellation and
// exit promptly, freeing the worker pool slot.
func (d *Dispatcher) safeCall(ctx context.Context, req *protocol.RequestFrame, handler HandlerFunc) *protocol.ResponseFrame {
	type result struct {
		resp *protocol.ResponseFrame
	}
	ch := make(chan result, 1)

	// Derive a cancellable context so we can signal the handler to stop
	// when the caller's deadline fires and we return a timeout response.
	handlerCtx, handlerCancel := context.WithCancel(ctx)

	run := func() {
		defer handlerCancel()
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("handler panic", "method", req.Method, "panic", r,
					"stack", string(debug.Stack()))
				ch <- result{resp: rpcerr.Newf(protocol.ErrUnavailable, "internal error handling %q", req.Method).Response(req.ID)}
			}
		}()
		ch <- result{resp: handler(handlerCtx, req)}
	}

	if pool := d.pool.Load(); pool != nil {
		pool.Submit(run)
	} else {
		go run()
	}

	select {
	case r := <-ch:
		return r.resp
	case <-ctx.Done():
		// Cancel the handler's context so it can observe the cancellation
		// and release the worker pool slot promptly.
		handlerCancel()
		d.logger.Warn("handler timeout", "method", req.Method)
		return rpcerr.Newf(protocol.ErrAgentTimeout, "handler %q did not complete within deadline", req.Method).Response(req.ID)
	}
}
