package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mockBtwHandler implements the BtwDeps.Chat interface for testing.
type mockBtwHandler struct {
	result string
	err    error
}

func (m *mockBtwHandler) HandleBtw(_ context.Context, _, _ string) (string, error) {
	return m.result, m.err
}

// ---------------------------------------------------------------------------
// Methods
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// BtwMethods
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// chat.btw handler
// ---------------------------------------------------------------------------

func TestChatBtw_MissingQuestion(t *testing.T) {
	handlers := BtwMethods(BtwDeps{
		Chat: &mockBtwHandler{result: "answer"},
	})
	handler := handlers["chat.btw"]

	req := &protocol.RequestFrame{
		ID:     "test-1",
		Params: json.RawMessage(`{"sessionKey":"sess-1"}`),
	}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for missing question")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("got %+v, want MISSING_PARAM", resp.Error)
	}
}

func TestChatBtw_Success(t *testing.T) {
	handlers := BtwMethods(BtwDeps{
		Chat: &mockBtwHandler{result: "the answer is 42"},
	})
	handler := handlers["chat.btw"]

	req := &protocol.RequestFrame{
		ID:     "test-5",
		Params: json.RawMessage(`{"question":"what is the answer?","sessionKey":"sess-1"}`),
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("got error: %+v, want OK", resp.Error)
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Text != "the answer is 42" {
		t.Fatalf("got %q, want text='the answer is 42'", payload.Text)
	}
}

func TestChatBtw_HandlerError(t *testing.T) {
	handlers := BtwMethods(BtwDeps{
		Chat: &mockBtwHandler{err: errors.New("model unavailable")},
	})
	handler := handlers["chat.btw"]

	req := &protocol.RequestFrame{
		ID:     "test-6",
		Params: json.RawMessage(`{"question":"hello?","sessionKey":"sess-1"}`),
	}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error from handler failure")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Fatalf("got %+v, want DEPENDENCY_FAILED", resp.Error)
	}
}

func TestChatBtw_BroadcasterCalledOnSuccess(t *testing.T) {
	var broadcasted bool
	handlers := BtwMethods(BtwDeps{
		Chat: &mockBtwHandler{result: "answer"},
		Broadcaster: func(event string, payload any) (int, []error) {
			broadcasted = true
			if event != "chat.side_result" {
				t.Fatalf("got %s, want event=chat.side_result", event)
			}
			m, ok := payload.(map[string]any)
			if !ok {
				t.Fatal("expected map payload")
			}
			if m["kind"] != "btw" {
				t.Fatalf("got %v, want kind=btw", m["kind"])
			}
			return 1, nil
		},
	})
	handler := handlers["chat.btw"]

	req := &protocol.RequestFrame{
		ID:     "test-7",
		Params: json.RawMessage(`{"question":"test?","sessionKey":"sess-1"}`),
	}
	handler(context.Background(), req)
	if !broadcasted {
		t.Fatal("expected broadcaster to be called")
	}
}
