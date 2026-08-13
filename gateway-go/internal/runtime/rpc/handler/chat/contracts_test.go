package chat

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func requestFrame(id, params string) *protocol.RequestFrame {
	return &protocol.RequestFrame{ID: id, Params: json.RawMessage(params)}
}

func decodeResponsePayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("decode payload %q: %v", resp.Payload, err)
	}
	return out
}

func requireRPCErrorCode(t *testing.T, resp *protocol.ResponseFrame, code string) {
	t.Helper()
	if resp == nil || resp.OK || resp.Error == nil {
		t.Fatalf("response = %+v, want error %s", resp, code)
	}
	if resp.Error.Code != code {
		t.Fatalf("error code = %s, want %s: %+v", resp.Error.Code, code, resp.Error)
	}
}

func TestBtwMethodsContract(t *testing.T) {
	methods := BtwMethods(BtwDeps{})
	if len(methods) != 1 || methods["chat.btw"] == nil {
		t.Fatalf("methods = %+v", methods)
	}

	tests := []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed json", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing question", params: `{"sessionKey":"s"}`, wantCode: protocol.ErrMissingParam},
		{name: "blank question", params: `{"question":"  \n ","sessionKey":"s"}`, wantCode: protocol.ErrMissingParam},
		{name: "missing session", params: `{"question":"q"}`, wantCode: protocol.ErrMissingParam},
		{name: "blank session", params: `{"question":"q","sessionKey":" \t "}`, wantCode: protocol.ErrMissingParam},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := methods["chat.btw"](context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}

	resp := methods["chat.btw"](context.Background(), requestFrame("id", `{"question":"q","sessionKey":"s"}`))
	requireRPCErrorCode(t, resp, protocol.ErrUnavailable)
}

type recordingBtwHandler struct {
	mu       sync.Mutex
	session  string
	question string
	result   string
	err      error
}

func (h *recordingBtwHandler) HandleBtw(_ context.Context, session, question string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.session = session
	h.question = question
	return h.result, h.err
}

func TestBtwHandlerTrimsInputAndBroadcastsContract(t *testing.T) {
	handler := &recordingBtwHandler{result: "answer"}
	var event string
	var payload map[string]any
	methods := BtwMethods(BtwDeps{
		Chat: handler,
		Broadcaster: func(gotEvent string, gotPayload events.EventPayload) (int, []error) {
			event = gotEvent
			_ = json.Unmarshal(gotPayload.Bytes(), &payload)
			return 1, nil
		},
	})
	resp := methods["chat.btw"](context.Background(), requestFrame("req-1", `{"question":"  what now?  ","sessionKey":"  client:main  "}`))
	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	handler.mu.Lock()
	if handler.question != "what now?" || handler.session != "client:main" {
		t.Fatalf("handler input = %q/%q", handler.session, handler.question)
	}
	handler.mu.Unlock()
	got := decodeResponsePayload[struct {
		Text string `json:"text"`
	}](t, resp)
	if got.Text != "answer" {
		t.Fatalf("text = %q", got.Text)
	}
	if event != "chat.side_result" {
		t.Fatalf("event = %q", event)
	}
	if payload["kind"] != "btw" || payload["sessionKey"] != "client:main" || payload["question"] != "what now?" || payload["text"] != "answer" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestBtwHandlerDependencyErrorDoesNotBroadcast(t *testing.T) {
	handler := &recordingBtwHandler{err: errors.New("model down")}
	broadcasts := 0
	method := BtwMethods(BtwDeps{
		Chat: handler,
		Broadcaster: func(string, events.EventPayload) (int, []error) {
			broadcasts++
			return 0, nil
		},
	})["chat.btw"]
	resp := method(context.Background(), requestFrame("id", `{"question":"q","sessionKey":"s"}`))
	requireRPCErrorCode(t, resp, protocol.ErrDependencyFailed)
	if broadcasts != 0 {
		t.Fatalf("broadcasts = %d", broadcasts)
	}
}

func TestMethodsNilContract(t *testing.T) {
	if got := Methods(Deps{}); got != nil {
		t.Fatalf("Methods nil chat = %+v", got)
	}
}
