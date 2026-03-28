package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// callMethod is a test helper that invokes a named handler from a map.
func callMethod(m map[string]rpcutil.HandlerFunc, method string, params any) *protocol.ResponseFrame {
	raw, _ := json.Marshal(params)
	req := &protocol.RequestFrame{ID: "t1", Method: method, Params: json.RawMessage(raw)}
	h, ok := m[method]
	if !ok {
		return nil
	}
	return h(context.Background(), req)
}

// ---------------------------------------------------------------------------
// Methods (session management)
// ---------------------------------------------------------------------------

func TestMethods_returnsHandlers(t *testing.T) {
	m := Methods(Deps{})
	for _, name := range []string{
		"sessions.patch",
		"sessions.reset",
		"sessions.preview",
		"sessions.resolve",
		"sessions.compact",
		"sessions.repair",
		"sessions.overflow_check",
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestSessionsPatch_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.patch", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsReset_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.reset", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsPreview_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.preview", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsResolve_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.resolve", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

// ---------------------------------------------------------------------------
// ExecMethods
// ---------------------------------------------------------------------------

func TestExecMethods_nilChat(t *testing.T) {
	m := ExecMethods(ExecDeps{})
	if m != nil {
		t.Fatal("expected nil when Chat is nil")
	}
}
