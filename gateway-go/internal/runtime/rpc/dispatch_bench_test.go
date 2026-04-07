package rpc

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func BenchmarkDispatchHit(b *testing.B) {
	d := NewDispatcher(slog.Default())
	d.Register("bench.echo", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, nil)
		return resp
	})

	req := &protocol.RequestFrame{
		ID:     "bench-1",
		Method: "bench.echo",
	}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		d.Dispatch(ctx, req)
	}
}

func BenchmarkDispatchMiss(b *testing.B) {
	d := NewDispatcher(slog.Default())
	req := &protocol.RequestFrame{
		ID:     "bench-1",
		Method: "nonexistent.method",
	}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		d.Dispatch(ctx, req)
	}
}

func BenchmarkRegister(b *testing.B) {
	d := NewDispatcher(slog.Default())
	handler := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, nil)
		return resp
	}

	b.ResetTimer()
	for range b.N {
		d.Register("bench.method", handler)
	}
}

// BenchmarkDispatchConcurrent measures dispatch under contention from
// multiple goroutines (typical pattern: parallel RPC handlers).
func BenchmarkDispatchConcurrent(b *testing.B) {
	d := NewDispatcher(slog.Default())
	methods := []string{"session.list", "session.get", "chat.send", "health", "memory.search"}
	for _, m := range methods {
		d.Register(m, func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			resp, _ := protocol.NewResponseOK(req.ID, nil)
			return resp
		})
	}

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &protocol.RequestFrame{
				ID:     "bench-1",
				Method: methods[i%len(methods)],
			}
			d.Dispatch(ctx, req)
			i++
		}
	})
}

// BenchmarkDispatchHighFanout measures dispatch with a large method registry
// (130+ methods like production) to check map lookup degradation.
func BenchmarkDispatchHighFanout(b *testing.B) {
	d := NewDispatcher(slog.Default())
	handler := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, nil)
		return resp
	}
	// Simulate production: 130 registered methods.
	for i := range 130 {
		d.Register(fmt.Sprintf("domain%d.method%d", i/10, i%10), handler)
	}
	d.Register("session.list", handler)

	req := &protocol.RequestFrame{ID: "bench-1", Method: "session.list"}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		d.Dispatch(ctx, req)
	}
}
