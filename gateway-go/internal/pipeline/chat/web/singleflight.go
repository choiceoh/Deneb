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

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()

	return c.val, c.err
}
