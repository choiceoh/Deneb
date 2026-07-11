package process

import (
	"context"
	"sync"
)

// managerRuntime owns every goroutine whose lifetime belongs to Manager rather
// than to an individual foreground caller. Registration and shutdown share the
// same mutex so Wait never races with a late WaitGroup.Add.
type managerRuntime struct {
	ctx    context.Context
	cancel context.CancelFunc

	stopOnce sync.Once
	mu       sync.Mutex
	stopped  bool
	wg       sync.WaitGroup
}

func newManagerRuntime() *managerRuntime {
	// cancel is retained by managerRuntime and deterministically called by stop.
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: lifecycle owner stores and calls cancel in stop.
	return &managerRuntime{ctx: ctx, cancel: cancel}
}

// goRun registers fn unless shutdown has begun. The goroutine receives the
// manager lifecycle context and is always joined by stop.
func (r *managerRuntime) goRun(fn func(context.Context)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn(r.ctx)
	}()
	return true
}

// goDetached preserves caller context values while deliberately detaching its
// cancellation and deadline. Cancellation comes from the manager lifecycle, so
// background commands outlive an RPC but not Manager.Stop.
func (r *managerRuntime) goDetached(caller context.Context, fn func(context.Context)) bool {
	if caller == nil {
		caller = context.Background()
	}
	values := context.WithoutCancel(caller)
	return r.goRun(func(lifecycleCtx context.Context) {
		fn(managerRunContext{Context: lifecycleCtx, values: values})
	})
}

// stop prevents new registrations, cancels all registered work, and waits for
// it to leave. sync.Once makes concurrent callers wait for the same shutdown.
func (r *managerRuntime) stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		r.cancel()
		r.mu.Unlock()

		r.wg.Wait()
	})
}

// managerRunContext takes cancellation/deadline from the manager lifecycle and
// values from the detached caller context.
type managerRunContext struct {
	context.Context
	values context.Context
}

// Value returns a run-context value while preserving cancellation semantics.
func (c managerRunContext) Value(key any) any {
	return c.values.Value(key)
}
