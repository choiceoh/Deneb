package web

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A panic in fn must not poison the key. Before the defer fix, an unwind
// skipped wg.Done()/delete, so the *call stayed in g.calls with its WaitGroup
// at 1 and every later caller for that key blocked on c.wg.Wait() forever.
func TestSingleflight_PanicDoesNotPoisonKey(t *testing.T) {
	var g singleflight

	// First call panics; the panic must still propagate to the caller.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the panic to propagate out of do()")
			}
		}()
		_, _ = g.do("k", func() (any, error) { panic("boom") })
	}()

	// Cleanup must have run during the unwind, so the key is gone and a second
	// call with the same key executes fresh instead of blocking forever.
	type res struct {
		v   any
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := g.do("k", func() (any, error) { return "ok", nil })
		ch <- res{v, err}
	}()
	select {
	case r := <-ch:
		if r.v != "ok" || r.err != nil {
			t.Fatalf("second do() = (%v, %v), want (ok, <nil>)", r.v, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second do() hung — the panic poisoned the key")
	}

	g.mu.Lock()
	_, stuck := g.calls["k"]
	g.mu.Unlock()
	if stuck {
		t.Fatal("a *call was left in g.calls after the panic — key poisoned")
	}
}

// Concurrent callers with the same key collapse into a single execution and
// all observe the same result.
func TestSingleflight_DedupesConcurrentCalls(t *testing.T) {
	var g singleflight
	var runs atomic.Int32

	const n = 8
	start := make(chan struct{})
	results := make([]any, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all at once so they contend on the same key
			v, _ := g.do("same", func() (any, error) {
				runs.Add(1)
				time.Sleep(20 * time.Millisecond) // hold the in-flight window open
				return "shared", nil
			})
			results[i] = v
		}(i)
	}
	close(start)
	wg.Wait()

	if r := runs.Load(); r < 1 || r >= n {
		t.Fatalf("fn ran %d times, want fewer than %d (calls should collapse)", r, n)
	}
	for i, v := range results {
		if v != "shared" {
			t.Errorf("results[%d] = %v, want shared", i, v)
		}
	}
}
