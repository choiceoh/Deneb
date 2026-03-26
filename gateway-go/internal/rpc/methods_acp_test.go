package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testACPDeps() *ACPDeps {
	registry := autoreply.NewACPRegistry()
	bindings := autoreply.NewSessionBindingService()
	sessions := session.NewManager()

	deps := &ACPDeps{
		Registry: registry,
		Bindings: bindings,
		Sessions: sessions,
		Infra: &autoreply.SubagentInfraDeps{
			ACPRegistry: registry,
		},
	}
	deps.SetEnabled(true)
	return deps
}

func testACPDispatcher(deps *ACPDeps) *Dispatcher {
	d := NewDispatcher(testLogger())
	RegisterACPMethods(d, deps)
	return d
}

func dispatchACP(t *testing.T, d *Dispatcher, method string, params any) *protocol.ResponseFrame {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}
	req := &protocol.RequestFrame{Type: "req", ID: "acp-test-1", Method: method, Params: raw}
	return d.Dispatch(context.Background(), req)
}

func requireOK(t *testing.T, resp *protocol.ResponseFrame) {
	t.Helper()
	if !resp.OK {
		errMsg := "unknown error"
		if resp.Error != nil {
			errMsg = fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		t.Fatalf("expected OK response, got error: %s", errMsg)
	}
}

func requireError(t *testing.T, resp *protocol.ResponseFrame, expectedCode string) {
	t.Helper()
	if resp.OK {
		t.Fatalf("expected error response with code %q, got OK", expectedCode)
	}
	if resp.Error == nil {
		t.Fatalf("expected error response, got nil error")
	}
	if resp.Error.Code != expectedCode {
		t.Fatalf("expected error code %q, got %q: %s", expectedCode, resp.Error.Code, resp.Error.Message)
	}
}

func unmarshalPayload(t *testing.T, resp *protocol.ResponseFrame) map[string]any {
	t.Helper()
	requireOK(t, resp)
	var result map[string]any
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	return result
}

// --- acp.status ---

func TestACPStatus(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.status", nil)
	result := unmarshalPayload(t, resp)

	if result["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", result["enabled"])
	}
	if result["totalAgents"] != float64(0) {
		t.Errorf("expected totalAgents=0, got %v", result["totalAgents"])
	}
}

func TestACPStatus_WithAgents(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	deps.Registry.Register(autoreply.ACPAgent{
		ID: "agent-1", Status: "running", SessionKey: "acp:test:agent-1",
	})
	deps.Registry.Register(autoreply.ACPAgent{
		ID: "agent-2", Status: "done", SessionKey: "acp:test:agent-2",
	})

	resp := dispatchACP(t, d, "acp.status", nil)
	result := unmarshalPayload(t, resp)

	if result["totalAgents"] != float64(2) {
		t.Errorf("expected totalAgents=2, got %v", result["totalAgents"])
	}
	if result["activeAgents"] != float64(1) {
		t.Errorf("expected activeAgents=1, got %v", result["activeAgents"])
	}
}

// --- acp.start / acp.stop ---

func TestACPStartStop(t *testing.T) {
	deps := testACPDeps()
	deps.SetEnabled(false)
	d := testACPDispatcher(deps)

	// Start.
	resp := dispatchACP(t, d, "acp.start", nil)
	result := unmarshalPayload(t, resp)
	if result["enabled"] != true {
		t.Errorf("expected enabled=true after start")
	}
	if !deps.IsEnabled() {
		t.Error("expected deps.IsEnabled() = true after start")
	}

	// Stop.
	resp = dispatchACP(t, d, "acp.stop", nil)
	result = unmarshalPayload(t, resp)
	if result["enabled"] != false {
		t.Errorf("expected enabled=false after stop")
	}
	if deps.IsEnabled() {
		t.Error("expected deps.IsEnabled() = false after stop")
	}
}

// --- acp.list ---

func TestACPList_Empty(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.list", nil)
	result := unmarshalPayload(t, resp)

	if result["count"] != float64(0) {
		t.Errorf("expected count=0, got %v", result["count"])
	}
}

func TestACPList_WithParentFilter(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	deps.Registry.Register(autoreply.ACPAgent{
		ID: "child-1", ParentID: "parent-a", Status: "running",
	})
	deps.Registry.Register(autoreply.ACPAgent{
		ID: "child-2", ParentID: "parent-b", Status: "running",
	})

	resp := dispatchACP(t, d, "acp.list", map[string]string{"parentId": "parent-a"})
	result := unmarshalPayload(t, resp)

	if result["count"] != float64(1) {
		t.Errorf("expected count=1 for parent-a filter, got %v", result["count"])
	}
}

// --- acp.spawn ---

func TestACPSpawn_Success(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.spawn", map[string]string{
		"role": "test-worker",
	})
	result := unmarshalPayload(t, resp)

	if result["agentId"] == nil || result["agentId"] == "" {
		t.Error("expected agentId in spawn result")
	}
	if result["sessionKey"] == nil || result["sessionKey"] == "" {
		t.Error("expected sessionKey in spawn result")
	}
}

func TestACPSpawn_MissingRole(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.spawn", map[string]string{})
	requireError(t, resp, protocol.ErrMissingParam)
}

func TestACPSpawn_Disabled(t *testing.T) {
	deps := testACPDeps()
	deps.SetEnabled(false)
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.spawn", map[string]string{"role": "worker"})
	requireError(t, resp, protocol.ErrFeatureDisabled)
}

// --- acp.kill ---

func TestACPKill_Success(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	deps.Registry.Register(autoreply.ACPAgent{
		ID: "agent-kill", Status: "running", SessionKey: "acp:test:agent-kill",
	})

	resp := dispatchACP(t, d, "acp.kill", map[string]string{"agentId": "agent-kill"})
	result := unmarshalPayload(t, resp)

	if result["killed"] != true {
		t.Error("expected killed=true")
	}

	// Verify agent status is killed.
	agent := deps.Registry.Get("agent-kill")
	if agent == nil {
		t.Fatal("expected agent still in registry")
	}
	if agent.Status != "killed" {
		t.Errorf("expected status=killed, got %s", agent.Status)
	}
}

func TestACPKill_NotFound(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.kill", map[string]string{"agentId": "nonexistent"})
	requireError(t, resp, protocol.ErrNotFound)
}

func TestACPKill_MissingParam(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.kill", map[string]string{})
	requireError(t, resp, protocol.ErrMissingParam)
}

// --- acp.send ---

func TestACPSend_ByAgentID(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	var sentKey, sentMsg string
	deps.SessionSendFn = func(sessionKey, message string) error {
		sentKey = sessionKey
		sentMsg = message
		return nil
	}

	deps.Registry.Register(autoreply.ACPAgent{
		ID: "agent-send", Status: "running", SessionKey: "acp:test:agent-send",
	})

	resp := dispatchACP(t, d, "acp.send", map[string]string{
		"agentId": "agent-send",
		"message": "hello agent",
	})
	requireOK(t, resp)

	if sentKey != "acp:test:agent-send" {
		t.Errorf("expected send to acp:test:agent-send, got %s", sentKey)
	}
	if sentMsg != "hello agent" {
		t.Errorf("expected message 'hello agent', got %s", sentMsg)
	}
}

func TestACPSend_BySessionKey(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	var sentKey string
	deps.SessionSendFn = func(sessionKey, message string) error {
		sentKey = sessionKey
		return nil
	}

	resp := dispatchACP(t, d, "acp.send", map[string]string{
		"sessionKey": "agent:main:main",
		"message":    "direct send",
	})
	requireOK(t, resp)

	if sentKey != "agent:main:main" {
		t.Errorf("expected send to agent:main:main, got %s", sentKey)
	}
}

func TestACPSend_MissingMessage(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.send", map[string]string{"agentId": "x"})
	requireError(t, resp, protocol.ErrMissingParam)
}

func TestACPSend_NoSendFn(t *testing.T) {
	deps := testACPDeps()
	deps.SessionSendFn = nil
	d := testACPDispatcher(deps)

	deps.Registry.Register(autoreply.ACPAgent{
		ID: "agent-x", Status: "running", SessionKey: "acp:test:agent-x",
	})

	resp := dispatchACP(t, d, "acp.send", map[string]string{
		"agentId": "agent-x",
		"message": "hi",
	})
	requireError(t, resp, protocol.ErrDependencyFailed)
}

// --- acp.bind / acp.unbind ---

func TestACPBindUnbind(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	// Bind.
	resp := dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "user1",
		"conversationId":   "thread-42",
		"targetSessionKey": "agent:main:main",
	})
	result := unmarshalPayload(t, resp)

	bindingID, ok := result["bindingId"].(string)
	if !ok || bindingID == "" {
		t.Fatal("expected bindingId in bind result")
	}

	// Unbind by binding ID.
	resp = dispatchACP(t, d, "acp.unbind", map[string]string{
		"bindingId": bindingID,
	})
	requireOK(t, resp)
}

func TestACPUnbind_ByConversation(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	// Bind first.
	dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "user1",
		"conversationId":   "thread-99",
		"targetSessionKey": "agent:main:main",
	})

	// Unbind by conversation.
	resp := dispatchACP(t, d, "acp.unbind", map[string]string{
		"channel":        "telegram",
		"accountId":      "user1",
		"conversationId": "thread-99",
	})
	requireOK(t, resp)
}

func TestACPBind_MissingTargetKey(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	resp := dispatchACP(t, d, "acp.bind", map[string]string{
		"channel": "telegram",
	})
	requireError(t, resp, protocol.ErrMissingParam)
}

// --- acp.bindings ---

func TestACPBindings_All(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	// Create some bindings.
	dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "u1",
		"conversationId":   "t1",
		"targetSessionKey": "agent:main:main",
	})
	dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "u1",
		"conversationId":   "t2",
		"targetSessionKey": "agent:design:main",
	})

	resp := dispatchACP(t, d, "acp.bindings", nil)
	result := unmarshalPayload(t, resp)

	if result["count"] != float64(2) {
		t.Errorf("expected count=2, got %v", result["count"])
	}
}

func TestACPBindings_FilterBySession(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "u1",
		"conversationId":   "t1",
		"targetSessionKey": "agent:main:main",
	})
	dispatchACP(t, d, "acp.bind", map[string]string{
		"channel":          "telegram",
		"accountId":        "u1",
		"conversationId":   "t2",
		"targetSessionKey": "agent:design:main",
	})

	resp := dispatchACP(t, d, "acp.bindings", map[string]string{"sessionKey": "agent:main:main"})
	result := unmarshalPayload(t, resp)

	if result["count"] != float64(1) {
		t.Errorf("expected count=1 for agent:main:main, got %v", result["count"])
	}
}

// --- Disabled state tests ---

func TestACPWriteOps_DisabledState(t *testing.T) {
	deps := testACPDeps()
	deps.SetEnabled(false)
	d := testACPDispatcher(deps)

	// All write operations should fail.
	writeMethods := []struct {
		method string
		params map[string]string
	}{
		{"acp.spawn", map[string]string{"role": "worker"}},
		{"acp.kill", map[string]string{"agentId": "x"}},
		{"acp.send", map[string]string{"agentId": "x", "message": "hi"}},
		{"acp.bind", map[string]string{"targetSessionKey": "x"}},
		{"acp.unbind", map[string]string{"bindingId": "x"}},
	}

	for _, tc := range writeMethods {
		t.Run(tc.method, func(t *testing.T) {
			resp := dispatchACP(t, d, tc.method, tc.params)
			requireError(t, resp, protocol.ErrFeatureDisabled)
		})
	}

	// Read operations should still work.
	readMethods := []string{"acp.status", "acp.list", "acp.bindings"}
	for _, method := range readMethods {
		t.Run(method, func(t *testing.T) {
			resp := dispatchACP(t, d, method, nil)
			requireOK(t, resp)
		})
	}
}

// --- Methods registration ---

func TestACPMethodsRegistered(t *testing.T) {
	deps := testACPDeps()
	d := testACPDispatcher(deps)

	expected := []string{
		"acp.status", "acp.start", "acp.stop",
		"acp.list", "acp.spawn", "acp.kill",
		"acp.send", "acp.bind", "acp.unbind", "acp.bindings",
	}

	methods := d.Methods()
	methodSet := make(map[string]bool)
	for _, m := range methods {
		methodSet[m] = true
	}

	for _, e := range expected {
		if !methodSet[e] {
			t.Errorf("missing expected ACP method: %s", e)
		}
	}
}
