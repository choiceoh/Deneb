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
		t.Fatalf("expected %d calls, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %s, want %s", i, order[i], v)
		}
	}
}

func TestAuth_PublicMethod(t *testing.T) {
	mw := Auth(map[string]bool{"health": true})
	handler := mw(okHandler)

	// Public method: no auth required.
	resp := handler(context.Background(), makeReq("1", "health"))
	if !resp.OK {
		t.Error("expected OK for public method")
	}
}

func TestAuth_ProtectedMethod_NoContext(t *testing.T) {
	mw := Auth(map[string]bool{"health": true})
	handler := mw(okHandler)

	resp := handler(context.Background(), makeReq("1", "sessions.list"))
	if resp.OK {
		t.Error("expected error for unauthenticated protected method")
	}
}

func TestAuth_ProtectedMethod_Authenticated(t *testing.T) {
	mw := Auth(map[string]bool{"health": true})
	handler := mw(okHandler)

	ctx := WithRequestContext(context.Background(), &RequestContext{
		ConnID:        "conn-1",
		Authenticated: true,
	})

	resp := handler(ctx, makeReq("1", "sessions.list"))
	if !resp.OK {
		t.Error("expected OK for authenticated request")
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

func TestRateLimit_AllowsNormal(t *testing.T) {
	mw := RateLimit(RateLimitConfig{MaxRequests: 5, WindowMs: 60000})
	handler := mw(okHandler)

	ctx := WithRequestContext(context.Background(), &RequestContext{ConnID: "c1"})
	for i := 0; i < 5; i++ {
		resp := handler(ctx, makeReq("1", "test"))
		if !resp.OK {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimit_BlocksExcess(t *testing.T) {
	mw := RateLimit(RateLimitConfig{MaxRequests: 2, WindowMs: 60000})
	handler := mw(okHandler)

	ctx := WithRequestContext(context.Background(), &RequestContext{ConnID: "c1"})
	handler(ctx, makeReq("1", "test"))
	handler(ctx, makeReq("2", "test"))

	resp := handler(ctx, makeReq("3", "test"))
	if resp.OK {
		t.Error("third request should be rate-limited")
	}
}

func TestRequestContext_RoundTrip(t *testing.T) {
	rc := &RequestContext{
		ConnID:        "conn-123",
		Role:          "operator",
		Authenticated: true,
		DeviceID:      "dev-1",
	}
	ctx := WithRequestContext(context.Background(), rc)
	got := GetRequestContext(ctx)

	if got == nil {
		t.Fatal("expected non-nil request context")
	}
	if got.ConnID != "conn-123" {
		t.Errorf("expected conn-123, got %s", got.ConnID)
	}
}
