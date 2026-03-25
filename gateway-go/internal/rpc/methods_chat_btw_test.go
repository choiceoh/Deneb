package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestHandleChatBtw_MissingParams(t *testing.T) {
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-1",
		Method: "chat.btw",
	})
	if resp.OK {
		t.Error("expected error for missing params")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("expected INVALID_REQUEST, got %v", resp.Error)
	}
}

func TestHandleChatBtw_MissingQuestion(t *testing.T) {
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{})
	params, _ := json.Marshal(map[string]any{"sessionKey": "sk-123"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-2",
		Method: "chat.btw",
		Params: params,
	})
	if resp.OK {
		t.Error("expected error for missing question")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %v", resp.Error)
	}
}

func TestHandleChatBtw_MissingSessionKey(t *testing.T) {
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{})
	params, _ := json.Marshal(map[string]any{"question": "what is 2+2?"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-3",
		Method: "chat.btw",
		Params: params,
	})
	if resp.OK {
		t.Error("expected error for missing sessionKey")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %v", resp.Error)
	}
}

func TestHandleChatBtw_NoBridge(t *testing.T) {
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{})
	params, _ := json.Marshal(map[string]any{"question": "what is 2+2?", "sessionKey": "sk-123"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-4",
		Method: "chat.btw",
		Params: params,
	})
	if resp.OK {
		t.Error("expected error when bridge unavailable")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE, got %v", resp.Error)
	}
}

// mockForwarder implements Forwarder for testing.
type mockBtwForwarder struct {
	resp *protocol.ResponseFrame
	err  error
}

func (m *mockBtwForwarder) Forward(_ context.Context, _ *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	return m.resp, m.err
}

func TestHandleChatBtw_BridgeSuccess(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"text": "4", "ts": 1234567890})
	fwd := &mockBtwForwarder{
		resp: &protocol.ResponseFrame{
			Type:    protocol.FrameTypeResponse,
			ID:      "test-5-btw",
			OK:      true,
			Payload: payload,
		},
	}

	var broadcastEvent string
	var broadcastPayload any
	broadcaster := func(event string, p any) (int, []error) {
		broadcastEvent = event
		broadcastPayload = p
		return 1, nil
	}

	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{
		Forwarder:   fwd,
		Broadcaster: broadcaster,
	})

	params, _ := json.Marshal(map[string]any{"question": "what is 2+2?", "sessionKey": "sk-123"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-5",
		Method: "chat.btw",
		Params: params,
	})

	if !resp.OK {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
	// Verify the response ID was re-mapped to the original request ID.
	if resp.ID != "test-5" {
		t.Errorf("expected response ID test-5, got %s", resp.ID)
	}

	// Verify broadcast was called with the correct event.
	if broadcastEvent != "chat.side_result" {
		t.Errorf("expected broadcast event chat.side_result, got %s", broadcastEvent)
	}
	bp, ok := broadcastPayload.(map[string]any)
	if !ok {
		t.Fatal("broadcast payload is not map[string]any")
	}
	if bp["kind"] != "btw" {
		t.Errorf("expected kind=btw, got %v", bp["kind"])
	}
	if bp["question"] != "what is 2+2?" {
		t.Errorf("expected question='what is 2+2?', got %v", bp["question"])
	}
	if bp["text"] != "4" {
		t.Errorf("expected text='4', got %v", bp["text"])
	}
}

func TestHandleChatBtw_BridgeError(t *testing.T) {
	fwd := &mockBtwForwarder{
		err: context.DeadlineExceeded,
	}

	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{Forwarder: fwd})

	params, _ := json.Marshal(map[string]any{"question": "what is 2+2?", "sessionKey": "sk-123"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-6",
		Method: "chat.btw",
		Params: params,
	})

	if resp.OK {
		t.Error("expected error on bridge failure")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("expected DEPENDENCY_FAILED, got %v", resp.Error)
	}
}
