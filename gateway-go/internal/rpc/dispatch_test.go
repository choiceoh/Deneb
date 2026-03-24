package rpc

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestDispatchRegisteredMethod(t *testing.T) {
	d := NewDispatcher(testLogger())
	d.Register("health", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"status": "ok"})
		return resp
	})

	req := &protocol.RequestFrame{Type: "req", ID: "1", Method: "health"}
	resp := d.Dispatch(context.Background(), req)

	if !resp.OK {
		t.Errorf("expected OK response, got error: %+v", resp.Error)
	}
}

func TestDispatchUnknownMethodNoForwarder(t *testing.T) {
	d := NewDispatcher(testLogger())
	req := &protocol.RequestFrame{Type: "req", ID: "2", Method: "unknown.method"}
	resp := d.Dispatch(context.Background(), req)

	if resp.OK {
		t.Error("expected error response for unknown method")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND error, got: %+v", resp.Error)
	}
}

type mockForwarder struct {
	called bool
}

func (m *mockForwarder) Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	m.called = true
	resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"forwarded": "true"})
	return resp, nil
}

func TestDispatchForwardsToBridge(t *testing.T) {
	d := NewDispatcher(testLogger())
	fwd := &mockForwarder{}
	d.SetForwarder(fwd)

	req := &protocol.RequestFrame{Type: "req", ID: "3", Method: "sessions.list"}
	resp := d.Dispatch(context.Background(), req)

	if !fwd.called {
		t.Error("forwarder should have been called")
	}
	if !resp.OK {
		t.Errorf("expected OK response from forwarder, got: %+v", resp.Error)
	}
}

func TestMethods(t *testing.T) {
	d := NewDispatcher(testLogger())
	d.Register("health", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return nil
	})
	d.Register("status", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return nil
	})

	methods := d.Methods()
	if len(methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(methods))
	}
}

func TestDispatchPanicRecovery(t *testing.T) {
	d := NewDispatcher(testLogger())
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
