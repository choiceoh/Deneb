package aurora_channel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestHandlePing(t *testing.T) {
	methods := Methods(Deps{Chat: nil})
	handler, ok := methods["aurora.ping"]
	if !ok {
		t.Fatal("aurora.ping handler not registered")
	}

	req := &protocol.RequestFrame{
		ID:     "test-1",
		Method: "aurora.ping",
	}
	resp := handler(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result["ok"])
	}
}

func TestHandleChatMissingMessage(t *testing.T) {
	methods := Methods(Deps{Chat: nil})
	handler := methods["aurora.chat"]

	params, _ := json.Marshal(map[string]string{"message": ""})
	req := &protocol.RequestFrame{
		ID:     "test-2",
		Method: "aurora.chat",
		Params: params,
	}
	resp := handler(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestHandleChatNilHandler(t *testing.T) {
	methods := Methods(Deps{Chat: nil})
	handler := methods["aurora.chat"]

	params, _ := json.Marshal(map[string]string{"message": "hello"})
	req := &protocol.RequestFrame{
		ID:     "test-3",
		Method: "aurora.chat",
		Params: params,
	}
	resp := handler(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for nil chat handler")
	}
}

func TestHandleMemoryMissingQuery(t *testing.T) {
	methods := Methods(Deps{Chat: nil})
	handler := methods["aurora.memory"]

	params, _ := json.Marshal(map[string]string{"query": ""})
	req := &protocol.RequestFrame{
		ID:     "test-4",
		Method: "aurora.memory",
		Params: params,
	}
	resp := handler(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for empty query")
	}
}
