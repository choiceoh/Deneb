package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// callMethod invokes a handler by method name from the given map.
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
// ExtendedMethods
// ---------------------------------------------------------------------------

func TestExtendedMethods_returnsCoreMethods(t *testing.T) {
	m := ExtendedMethods(ExtendedDeps{})
	// These methods are always registered regardless of optional deps.
	for _, name := range []string{"agent.status", "sessions.create", "sessions.lifecycle"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestExtendedMethods_noProcessHandlersWithNilDep(t *testing.T) {
	m := ExtendedMethods(ExtendedDeps{})
	for _, name := range []string{"process.exec", "process.kill", "process.get", "process.list"} {
		if _, ok := m[name]; ok {
			t.Errorf("handler %q should not be registered with nil Processes", name)
		}
	}
}

func TestExtendedMethods_noCronHandlersWithNilDep(t *testing.T) {
	m := ExtendedMethods(ExtendedDeps{})
	for _, name := range []string{"cron.list", "cron.get", "cron.unregister"} {
		if _, ok := m[name]; ok {
			t.Errorf("handler %q should not be registered with nil Cron", name)
		}
	}
}

func TestExtendedMethods_noHookHandlersWithNilDep(t *testing.T) {
	m := ExtendedMethods(ExtendedDeps{})
	for _, name := range []string{"hooks.list", "hooks.register", "hooks.unregister", "hooks.fire"} {
		if _, ok := m[name]; ok {
			t.Errorf("handler %q should not be registered with nil Hooks", name)
		}
	}
}

// ---------------------------------------------------------------------------
// CRUDMethods
// ---------------------------------------------------------------------------

func TestCRUDMethods_nilAgents(t *testing.T) {
	m := CRUDMethods(AgentsDeps{})
	if m != nil {
		t.Fatal("expected nil when Agents is nil")
	}
}
