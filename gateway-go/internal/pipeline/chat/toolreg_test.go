package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestToolRegistry_Execute(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		return string(input), nil
	})

	result := testutil.Must(reg.Execute(context.Background(), "echo", json.RawMessage(`"hello"`)))
	if result != `"hello"` {
		t.Errorf("result = %q, want %q", result, `"hello"`)
	}
}

func TestToolRegistry_UnknownTool(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

