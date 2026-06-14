package web

import "sync"

// singleflight collapses duplicate in-flight calls into one execution.
// Callers with the same key block until the first caller completes, then
// all receive the same result.
type singleflight struct {
	mu    sync.Mutex
	calls map[string]*call
}

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

// do executes fn once for a given key. Concurrent callers with the same key
// wait for the first execution and share its result.
func (g *singleflight) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*call)
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	// Clean up with defer so a panic in fn (e.g. a fault in the htmlmd parser
	// or a nil-deref) cannot poison the key. Without defer, an unwind
	// skips wg.Done()/delete, leaving the *call stuck in g.calls with its
	// WaitGroup at 1 — every later caller for this key then blocks on
	// c.wg.Wait() forever (bounded only by the turn deadline) and leaks a
	// goroutine each time. The panic still propagates to the caller's recover;
	// a concurrent waiter gets the zero result, which is correct degradation
	// versus a permanent hang. Mirrors x/sync/singleflight, which defers for
	// exactly this reason.
	defer func() {
		c.wg.Done()
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
	}()

	c.val, c.err = fn()
	return c.val, c.err
}
