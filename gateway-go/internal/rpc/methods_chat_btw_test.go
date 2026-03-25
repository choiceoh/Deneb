package rpc

import (
	"context"
	"encoding/json"
	"errors"
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

func TestHandleChatBtw_NoChatHandler(t *testing.T) {
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
		t.Error("expected error when chat handler unavailable")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE, got %v", resp.Error)
	}
}

// mockBtwChat implements the ChatBtwDeps.Chat interface for testing.
type mockBtwChat struct {
	text string
	err  error
}

func (m *mockBtwChat) HandleBtw(_ context.Context, _, _ string) (string, error) {
	return m.text, m.err
}

func TestHandleChatBtw_Success(t *testing.T) {
	var broadcastEvent string
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{
		Chat: &mockBtwChat{text: "4"},
		Broadcaster: func(event string, payload any) (int, []error) {
			broadcastEvent = event
			data, _ := payload.(map[string]any)
			if data["kind"] != "btw" {
				t.Errorf("expected kind=btw, got %v", data["kind"])
			}
			if data["text"] != "4" {
				t.Errorf("expected text=4, got %v", data["text"])
			}
			return 1, nil
		},
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
	if broadcastEvent != "chat.side_result" {
		t.Errorf("expected broadcast event chat.side_result, got %s", broadcastEvent)
	}
}

func TestHandleChatBtw_ChatError(t *testing.T) {
	d := NewDispatcher(nil)
	RegisterChatBtwMethods(d, ChatBtwDeps{
		Chat: &mockBtwChat{err: errors.New("model error")},
	})

	params, _ := json.Marshal(map[string]any{"question": "what is 2+2?", "sessionKey": "sk-123"})
	resp := d.Dispatch(context.Background(), &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "test-6",
		Method: "chat.btw",
		Params: params,
	})

	if resp.OK {
		t.Error("expected error on chat failure")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("expected DEPENDENCY_FAILED, got %v", resp.Error)
	}
}
