package rpc

import (
	"context"
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
	for i := 0; i < b.N; i++ {
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
	for i := 0; i < b.N; i++ {
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
	for i := 0; i < b.N; i++ {
		d.Register("bench.method", handler)
	}
}
