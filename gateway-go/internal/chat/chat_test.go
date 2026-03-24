package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type mockForwarder struct {
	lastReq *protocol.RequestFrame
	resp    *protocol.ResponseFrame
	err     error
}

func (m *mockForwarder) Forward(_ context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"status": "ok"})
	return resp, nil
}

func newTestHandler(forwarder Forwarder) *Handler {
	sessions := session.NewManager()
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	return NewHandler(sessions, forwarder, broadcastFn, nil, DefaultHandlerConfig())
}

func makeReq(id, method string, params any) *protocol.RequestFrame {
	raw, _ := json.Marshal(params)
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     id,
		Method: method,
		Params: raw,
	}
}

func TestChatSend_MissingSessionKey(t *testing.T) {
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.send", map[string]string{"message": "hi"})
	resp := h.Send(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing sessionKey")
	}
}

func TestChatSend_MissingMessage(t *testing.T) {
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.send", map[string]string{"sessionKey": "test"})
	resp := h.Send(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing message")
	}
}

func TestChatSend_NoForwarder(t *testing.T) {
	sessions := session.NewManager()
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sessions, nil, broadcastFn, nil, DefaultHandlerConfig())
	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey": "test-key",
		"message":    "hello",
	})
	resp := h.Send(context.Background(), req)
	if resp.OK {
		t.Error("expected error when no forwarder available")
	}
}

func TestChatSend_Success(t *testing.T) {
	fwd := &mockForwarder{}
	h := newTestHandler(fwd)
	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey": "test-key",
		"message":    "hello world",
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
	if fwd.lastReq == nil {
		t.Error("expected forward to be called")
	}
}

func TestChatSend_ConflictWhenRunning(t *testing.T) {
	fwd := &mockForwarder{}
	h := newTestHandler(fwd)
	// Create a session and set it to running.
	h.sessions.Create("running-key", session.KindDirect)
	h.sessions.ApplyLifecycleEvent("running-key", session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1000})

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey": "running-key",
		"message":    "hello",
	})
	resp := h.Send(context.Background(), req)
	if resp.OK {
		t.Error("expected conflict error for running session")
	}
}

func TestChatHistory_MissingSessionKey(t *testing.T) {
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.history", map[string]string{})
	resp := h.History(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing sessionKey")
	}
}

func TestChatHistory_NoForwarder(t *testing.T) {
	sessions := session.NewManager()
	h := NewHandler(sessions, nil, nil, nil, DefaultHandlerConfig())
	req := makeReq("1", "chat.history", map[string]any{"sessionKey": "test"})
	resp := h.History(context.Background(), req)
	if !resp.OK {
		t.Error("expected empty history when no forwarder")
	}
}

func TestChatAbort_NotFound(t *testing.T) {
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.abort", map[string]any{"clientRunId": "nonexistent"})
	resp := h.Abort(context.Background(), req)
	if resp.OK {
		t.Error("expected not found error")
	}
}

func TestChatInject_MissingParams(t *testing.T) {
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.inject", map[string]any{"sessionKey": "test"})
	resp := h.Inject(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing content")
	}
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"hello\tworld", "hello\tworld"},
		{"hello\nworld", "hello\nworld"},
		{"  trimmed  ", "trimmed"},
		{"hello\x00world", "helloworld"},       // null byte stripped
		{"hello\x07world", "helloworld"},       // bell stripped
		{"hello\x1bworld", "helloworld"},       // escape stripped
	}
	for _, tt := range tests {
		got := sanitizeInput(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeInput(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
