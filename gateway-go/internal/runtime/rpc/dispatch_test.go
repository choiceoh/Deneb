package rpc

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestDispatchRegisteredMethod(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	d.Register("health", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"status": "ok"})
		return resp
	})

	req := &protocol.RequestFrame{Type: "req", ID: "1", Method: "health"}
	resp := d.Dispatch(context.Background(), req)

	if !resp.OK {
		t.Errorf("got error: %+v, want OK response", resp.Error)
	}
}

func TestDispatchUnknownMethodReturnsNotFound(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	req := &protocol.RequestFrame{Type: "req", ID: "3", Method: "unknown.forwarded"}
	resp := d.Dispatch(context.Background(), req)

	if resp.OK {
		t.Error("expected error for unknown method")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND error, got: %+v", resp.Error)
	}
}

func TestDispatchTimeoutReturnsAgentTimeout(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	d.Register("slow", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		<-ctx.Done()
		return protocol.NewResponseError(req.ID, protocol.NewError(protocol.ErrUnavailable, "unexpected return"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cancel()

	req := &protocol.RequestFrame{Type: "req", ID: "timeout-1", Method: "slow"}
	resp := d.Dispatch(ctx, req)
	if resp.OK {
		t.Fatal("expected timeout error response")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrAgentTimeout {
		t.Fatalf("got %+v, want AGENT_TIMEOUT", resp.Error)
	}
}

func TestDispatchCanceledContextReturnsAgentTimeout(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	d.Register("cancelled", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		<-ctx.Done()
		return protocol.NewResponseError(req.ID, protocol.NewError(protocol.ErrUnavailable, "unexpected return"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before dispatch

	req := &protocol.RequestFrame{Type: "req", ID: "cancel-1", Method: "cancelled"}
	resp := d.Dispatch(ctx, req)
	if resp.OK {
		t.Fatal("expected timeout error response for canceled context")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrAgentTimeout {
		t.Fatalf("got %+v, want AGENT_TIMEOUT", resp.Error)
	}
}

func TestMethods(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	d.Register("health", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return nil
	})
	d.Register("status", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return nil
	})

	methods := d.Methods()
	if len(methods) != 2 {
		t.Errorf("got %d, want 2 methods", len(methods))
	}
}

func TestDispatchPanicRecovery(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	d.Register("crasher", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		panic("intentional test panic")
	})

	req := &protocol.RequestFrame{Type: "req", ID: "panic-1", Method: "crasher"}
	resp := d.Dispatch(context.Background(), req)

	if resp.OK {
		t.Error("expected error response after panic")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE error, got: %+v", resp.Error)
	}
}

func TestDispatchWithMiddleware(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())

	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	mw1 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			record("mw1-before")
			resp := next(ctx, req)
			record("mw1-after")
			return resp
		}
	}
	mw2 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			record("mw2-before")
			resp := next(ctx, req)
			record("mw2-after")
			return resp
		}
	}

	d.UseMiddleware(mw1, mw2)
	d.Register("test.mw", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		record("handler")
		resp, _ := protocol.NewResponseOK(req.ID, "ok")
		return resp
	})

	req := &protocol.RequestFrame{ID: "mw-1", Method: "test.mw"}
	resp := d.Dispatch(context.Background(), req)
	if !resp.OK {
		t.Fatalf("got error: %+v, want OK", resp.Error)
	}

	// Wait for goroutine to complete and record all entries.
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	want := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(want) {
		t.Fatalf("got %d: %v, want %d calls", len(order), order, len(want))
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, order[i], w, order)
		}
	}
}

func TestDispatchWithWorkerPool(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())
	pool := NewWorkerPool(2)
	d.SetWorkerPool(pool)

	var maxConcurrent atomic.Int64
	var running atomic.Int64
	var wg sync.WaitGroup

	d.Register("slow.work", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		cur := running.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		wg.Done()
		resp, _ := protocol.NewResponseOK(req.ID, nil)
		return resp
	})

	// Fire 6 concurrent requests through a pool of size 2.
	for i := range 6 {
		wg.Add(1)
		go func(id int) {
			req := &protocol.RequestFrame{ID: "wp-" + string(rune('0'+id)), Method: "slow.work"}
			d.Dispatch(context.Background(), req)
		}(i)
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got > 2 {
		t.Errorf("got %d, want max concurrency ≤2 with pool size 2", got)
	}

	// pool.done is incremented in the Submit defer AFTER the handler returns,
	// so it may lag slightly behind wg.Done() which fires inside the handler.
	// Poll briefly to let the last goroutine's defer complete.
	deadline := time.After(2 * time.Second)
	for pool.Stats().Done != 6 {
		select {
		case <-deadline:
			t.Fatalf("got %d, want 6 done", pool.Stats().Done)
		default:
			runtime.Gosched()
		}
	}
}

func TestDispatchTimeoutCancelsHandler(t *testing.T) {
	d := NewDispatcher(rpctest.NewLogger())

	var handlerCanceled atomic.Bool
	d.Register("cancellable", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		<-ctx.Done()
		handlerCanceled.Store(true)
		resp, _ := protocol.NewResponseOK(req.ID, nil)
		return resp
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &protocol.RequestFrame{ID: "cancel-test", Method: "cancellable"}
	resp := d.Dispatch(ctx, req)

	if resp.Error == nil || resp.Error.Code != protocol.ErrAgentTimeout {
		t.Fatalf("expected AGENT_TIMEOUT, got: %+v", resp.Error)
	}

	// Give the handler goroutine time to observe cancellation.
	time.Sleep(20 * time.Millisecond)

	if !handlerCanceled.Load() {
		t.Error("handler did not observe context cancellation after timeout")
	}
}
