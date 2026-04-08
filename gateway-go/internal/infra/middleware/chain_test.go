package middleware

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func makeReq(id, method string) *protocol.RequestFrame {
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     id,
		Method: method,
	}
}

func okHandler(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	resp, _ := protocol.NewResponseOK(req.ID, "ok")
	return resp
}

func TestChain_Order(t *testing.T) {
	var order []string

	m1 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			order = append(order, "m1-before")
			resp := next(ctx, req)
			order = append(order, "m1-after")
			return resp
		}
	}

	m2 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			order = append(order, "m2-before")
			resp := next(ctx, req)
			order = append(order, "m2-after")
			return resp
		}
	}

	chained := Chain(m1, m2)(okHandler)
	chained(context.Background(), makeReq("1", "test"))

	expected := []string{"m1-before", "m2-before", "m2-after", "m1-after"}
	if len(order) != len(expected) {
		t.Fatalf("got %d, want %d calls", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %s, want %s", i, order[i], v)
		}
	}
}

func TestLogging_Middleware(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mw := Logging(logger)
	handler := mw(okHandler)

	resp := handler(context.Background(), makeReq("1", "test"))
	if !resp.OK {
		t.Error("expected OK")
	}
}
