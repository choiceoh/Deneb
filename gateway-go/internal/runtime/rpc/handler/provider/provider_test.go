package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestMethods_nilProviders(t *testing.T) {
	m := Methods(Deps{})
	if m != nil {
		t.Fatal("expected nil for nil Providers")
	}
}

func TestModelsMethods_returnsHandlers(t *testing.T) {
	m := ModelsMethods(ModelsDeps{})
	if m == nil {
		t.Fatal("expected non-nil handler map")
	}
	if _, ok := m["models.list"]; !ok {
		t.Fatal("missing models.list handler")
	}
}

func TestModelsList_nilProviders(t *testing.T) {
	handlers := ModelsMethods(ModelsDeps{})
	req := &protocol.RequestFrame{
		ID:     "test-1",
		Params: json.RawMessage(`{}`),
	}
	resp := handlers["models.list"](context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}
	var payload struct {
		Models []any `json:"models"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(payload.Models) != 0 {
		t.Errorf("expected empty models, got %d", len(payload.Models))
	}
}
