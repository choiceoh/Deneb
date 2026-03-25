package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

func newTestHandler() *Handler {
	sessions := session.NewManager()
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	return NewHandler(sessions, broadcastFn, nil, DefaultHandlerConfig())
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

func TestChatSend_NoForwarder_AsyncOK(t *testing.T) {
	// With native agent execution, Send works without a forwarder.
	// It starts an async run and returns immediately.
	sessions := session.NewManager()
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sessions, nil, broadcastFn, nil, DefaultHandlerConfig())
	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "test-key",
		"message":     "hello",
		"clientRunId": "run-1",
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Errorf("expected async start success, got error: %v", resp.Error)
	}
}

func TestChatSend_AsyncStart(t *testing.T) {
	// Send now returns immediately with {runId, status: "started"}.
	h := newTestHandler(&mockForwarder{})
	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "test-key",
		"message":     "hello world",
		"clientRunId": "run-123",
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
	// Verify the response contains the runId.
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to parse response payload: %v", err)
	}
	if payload["runId"] != "run-123" {
		t.Errorf("expected runId=run-123, got %v", payload["runId"])
	}
	if payload["status"] != "started" {
		t.Errorf("expected status=started, got %v", payload["status"])
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

// --- Sessions.* method tests ---

func TestSessionsSend_AsyncStart(t *testing.T) {
	h := newTestHandler(nil)
	req := makeReq("1", "sessions.send", map[string]any{
		"key":            "sess-1",
		"message":        "hello",
		"idempotencyKey": "idem-1",
	})
	resp := h.SessionsSend(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["status"] != "started" {
		t.Errorf("expected status=started, got %v", payload["status"])
	}
	if payload["runId"] != "idem-1" {
		t.Errorf("expected runId=idem-1, got %v", payload["runId"])
	}
}

func TestSessionsSend_MissingKey(t *testing.T) {
	h := newTestHandler(nil)
	req := makeReq("1", "sessions.send", map[string]any{"message": "hi"})
	resp := h.SessionsSend(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing key")
	}
}

func TestSessionsSteer_AppliesModel(t *testing.T) {
	h := newTestHandler(nil)
	req := makeReq("1", "sessions.steer", map[string]any{
		"key":   "sess-2",
		"model": "claude-opus-4-20250514",
	})
	resp := h.SessionsSteer(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["status"] != "started" {
		t.Errorf("expected status=started, got %v", payload["status"])
	}
}

func TestSessionsAbort_NoActiveRun(t *testing.T) {
	h := newTestHandler(nil)
	req := makeReq("1", "sessions.abort", map[string]any{"key": "no-such-session"})
	resp := h.SessionsAbort(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK with no-active-run, got error: %v", resp.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["status"] != "no-active-run" {
		t.Errorf("expected status=no-active-run, got %v", payload["status"])
	}
}

func TestSessionsAbort_ByRunID(t *testing.T) {
	h := newTestHandler(nil)
	// Start a run to create an abort entry.
	sendReq := makeReq("1", "sessions.send", map[string]any{
		"key":            "sess-abort",
		"message":        "hello",
		"idempotencyKey": "run-to-abort",
	})
	h.SessionsSend(context.Background(), sendReq)

	// Abort by runId.
	abortReq := makeReq("2", "sessions.abort", map[string]any{"runId": "run-to-abort"})
	resp := h.SessionsAbort(context.Background(), abortReq)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %v", resp.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["status"] != "aborted" {
		t.Errorf("expected status=aborted, got %v", payload["status"])
	}
	if payload["abortedRunId"] != "run-to-abort" {
		t.Errorf("expected abortedRunId=run-to-abort, got %v", payload["abortedRunId"])
	}
}

func TestSessionsAbort_MissingParams(t *testing.T) {
	h := newTestHandler(nil)
	req := makeReq("1", "sessions.abort", map[string]any{})
	resp := h.SessionsAbort(context.Background(), req)
	if resp.OK {
		t.Error("expected error for missing key and runId")
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
