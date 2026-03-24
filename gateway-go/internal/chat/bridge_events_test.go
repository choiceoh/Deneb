package chat

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// mockBroadcastRaw captures raw broadcast calls for testing.
type mockBroadcastRaw struct {
	mu    sync.Mutex
	calls []broadcastRawCall
}

type broadcastRawCall struct {
	event string
	data  []byte
}

func (m *mockBroadcastRaw) fn(event string, data []byte) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, broadcastRawCall{event: event, data: data})
	return 1
}

func (m *mockBroadcastRaw) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockBroadcastRaw) lastEvent() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return ""
	}
	return m.calls[len(m.calls)-1].event
}

func newBridgeTestHandler() *Handler {
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	return NewHandler(nil, nil, broadcastFn, testLogger(), DefaultHandlerConfig())
}

func TestHandleBridgeChatDoneEvent(t *testing.T) {
	h := newBridgeTestHandler()
	mock := &mockBroadcastRaw{}
	h.SetBroadcastRaw(mock.fn)

	payload, _ := json.Marshal(map[string]string{"clientRunId": "run-1", "state": "done"})
	ev := &protocol.EventFrame{
		Type:    protocol.FrameTypeEvent,
		Event:   "chat",
		Payload: payload,
	}
	h.HandleBridgeEvent(ev)

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 broadcast, got %d", mock.callCount())
	}
	if mock.lastEvent() != "chat" {
		t.Errorf("expected event 'chat', got %q", mock.lastEvent())
	}
}

func TestHandleBridgeChatDelta(t *testing.T) {
	h := newBridgeTestHandler()
	mock := &mockBroadcastRaw{}
	h.SetBroadcastRaw(mock.fn)

	payload, _ := json.Marshal(map[string]string{"runId": "run-1", "text": "Hello"})
	ev := &protocol.EventFrame{
		Type:    protocol.FrameTypeEvent,
		Event:   "chat.delta",
		Payload: payload,
	}
	h.HandleBridgeEvent(ev)

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 broadcast, got %d", mock.callCount())
	}
	if mock.lastEvent() != "chat.delta" {
		t.Errorf("expected event 'chat.delta', got %q", mock.lastEvent())
	}
}

func TestHandleBridgeNonChatEvent(t *testing.T) {
	h := newBridgeTestHandler()
	mock := &mockBroadcastRaw{}
	h.SetBroadcastRaw(mock.fn)

	payload, _ := json.Marshal(map[string]string{"key": "value"})
	ev := &protocol.EventFrame{
		Type:    protocol.FrameTypeEvent,
		Event:   "sessions.changed",
		Payload: payload,
	}
	h.HandleBridgeEvent(ev)

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 broadcast, got %d", mock.callCount())
	}
	if mock.lastEvent() != "sessions.changed" {
		t.Errorf("expected event 'sessions.changed', got %q", mock.lastEvent())
	}
}

func TestHandleBridgeEventNoBroadcastRaw(t *testing.T) {
	h := newBridgeTestHandler()
	// No broadcastRaw set — should not panic.
	payload, _ := json.Marshal(map[string]string{"state": "done"})
	ev := &protocol.EventFrame{
		Type: protocol.FrameTypeEvent, Event: "chat", Payload: payload,
	}
	h.HandleBridgeEvent(ev) // should be a no-op
}

func TestHandleBridgeChatDoneCleansUpAbort(t *testing.T) {
	h := newBridgeTestHandler()
	mock := &mockBroadcastRaw{}
	h.SetBroadcastRaw(mock.fn)

	// Register an abort entry manually.
	h.abortMu.Lock()
	h.abortMap["run-1"] = &AbortEntry{
		SessionKey: "sk",
		ClientRun:  "run-1",
		CancelFn:   func() {},
	}
	h.abortMu.Unlock()

	// Send a "done" event.
	payload, _ := json.Marshal(map[string]string{"clientRunId": "run-1", "state": "done"})
	ev := &protocol.EventFrame{
		Type: protocol.FrameTypeEvent, Event: "chat", Payload: payload,
	}
	h.HandleBridgeEvent(ev)

	// Abort entry should be cleaned up.
	h.abortMu.Lock()
	_, exists := h.abortMap["run-1"]
	h.abortMu.Unlock()
	if exists {
		t.Error("expected abort entry to be cleaned up after done event")
	}
}
