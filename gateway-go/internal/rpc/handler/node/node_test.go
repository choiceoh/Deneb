package node

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/device"
	nodemod "github.com/choiceoh/deneb/gateway-go/internal/node"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpctest"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

var (
	call    = rpctest.Call
	mustOK  = rpctest.MustOK
	mustErr = rpctest.MustErr
	result  = rpctest.Result
)

func newNodeDeps() Deps {
	return Deps{Nodes: nodemod.NewManager()}
}

func newDeviceDeps() DeviceDeps {
	return DeviceDeps{Devices: device.NewManager()}
}

// ─── Methods registration ─────────────────────────────────────────────────────

func TestMethods_nilManagerReturnsNil(t *testing.T) {
	m := Methods(Deps{Nodes: nil})
	if m != nil {
		t.Error("expected nil map when Nodes is nil")
	}
}

func TestMethods_registersAllHandlers(t *testing.T) {
	m := Methods(newNodeDeps())
	expected := []string{
		"node.pair.request",
		"node.pair.list",
		"node.pair.approve",
		"node.pair.reject",
		"node.pair.verify",
		"node.list",
		"node.describe",
		"node.rename",
		"node.invoke",
		"node.invoke.result",
		"node.canvas.capability.refresh",
		"node.pending.pull",
		"node.pending.ack",
		"node.pending.drain",
		"node.pending.enqueue",
		"node.event",
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestDeviceMethods_nilManagerReturnsNil(t *testing.T) {
	m := DeviceMethods(DeviceDeps{Devices: nil})
	if m != nil {
		t.Error("expected nil map when Devices is nil")
	}
}

func TestDeviceMethods_registersAllHandlers(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	expected := []string{
		"device.pair.list",
		"device.pair.approve",
		"device.pair.reject",
		"device.pair.remove",
		"device.token.rotate",
		"device.token.revoke",
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

// ─── node.pair.request ───────────────────────────────────────────────────────

func TestNodePairRequest_missingNodeID(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.request", map[string]any{})
	mustErr(t, resp)
}

func TestNodePairRequest_success(t *testing.T) {
	var broadcastCalls []string
	deps := Deps{
		Nodes: nodemod.NewManager(),
		Broadcaster: func(event string, _ any) (int, []error) {
			broadcastCalls = append(broadcastCalls, event)
			return 1, nil
		},
	}
	m := Methods(deps)
	resp := call(m, "node.pair.request", map[string]any{
		"nodeId":      "node-abc",
		"displayName": "My Mac",
	})
	mustOK(t, resp)
	r := result(t, resp)
	if r["requestId"] == "" {
		t.Errorf("expected requestId: %v", r)
	}
	if len(broadcastCalls) == 0 || broadcastCalls[0] != "node.pair.requested" {
		t.Errorf("expected broadcast: %v", broadcastCalls)
	}
}

func TestNodePairRequest_silentNoBroadcast(t *testing.T) {
	var broadcastCalled bool
	deps := Deps{
		Nodes: nodemod.NewManager(),
		Broadcaster: func(_ string, _ any) (int, []error) {
			broadcastCalled = true
			return 1, nil
		},
	}
	m := Methods(deps)
	call(m, "node.pair.request", map[string]any{
		"nodeId": "node-xyz",
		"silent": true,
	})
	if broadcastCalled {
		t.Error("silent=true should suppress broadcast")
	}
}

// ─── node.pair.list ──────────────────────────────────────────────────────────

func TestNodePairList_empty(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.list", nil)
	mustOK(t, resp)
	r := result(t, resp)
	pending := r["pending"].([]any)
	paired := r["paired"].([]any)
	if len(pending) != 0 || len(paired) != 0 {
		t.Errorf("expected empty lists: %v", r)
	}
}

func TestNodePairList_afterRequest(t *testing.T) {
	m := Methods(newNodeDeps())
	call(m, "node.pair.request", map[string]any{"nodeId": "n1"})

	resp := call(m, "node.pair.list", nil)
	mustOK(t, resp)
	r := result(t, resp)
	pending := r["pending"].([]any)
	if len(pending) == 0 {
		t.Error("expected pending request in list")
	}
}

// ─── node.pair.approve ───────────────────────────────────────────────────────

func TestNodePairApprove_missingRequestID(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.approve", map[string]any{})
	mustErr(t, resp)
}

func TestNodePairApprove_notFound(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.approve", map[string]any{"requestId": "no-such-id"})
	mustErr(t, resp)
}

func TestNodePairApprove_success(t *testing.T) {
	deps := newNodeDeps()
	m := Methods(deps)

	// First create a pair request.
	pairResp := call(m, "node.pair.request", map[string]any{"nodeId": "n1"})
	mustOK(t, pairResp)
	pairResult := result(t, pairResp)
	requestID := pairResult["requestId"].(string)

	// Now approve it.
	resp := call(m, "node.pair.approve", map[string]any{"requestId": requestID})
	mustOK(t, resp)
	r := result(t, resp)
	if r["node"] == nil {
		t.Errorf("expected node in result: %v", r)
	}
}

// ─── node.pair.reject ────────────────────────────────────────────────────────

func TestNodePairReject_missingRequestID(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.reject", map[string]any{})
	mustErr(t, resp)
}

func TestNodePairReject_notFound(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.reject", map[string]any{"requestId": "no-such"})
	mustErr(t, resp)
}

func TestNodePairReject_success(t *testing.T) {
	deps := newNodeDeps()
	m := Methods(deps)

	pairResp := call(m, "node.pair.request", map[string]any{"nodeId": "n2"})
	pairResult := result(t, pairResp)
	requestID := pairResult["requestId"].(string)

	resp := call(m, "node.pair.reject", map[string]any{"requestId": requestID})
	mustOK(t, resp)
}

// ─── node.pair.verify ────────────────────────────────────────────────────────

func TestNodePairVerify_missingParams(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.verify", map[string]any{"nodeId": "n1"})
	mustErr(t, resp)
}

func TestNodePairVerify_invalidToken(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pair.verify", map[string]any{
		"nodeId": "n1",
		"token":  "bad-token",
	})
	mustOK(t, resp)
	r := result(t, resp)
	if r["valid"].(bool) != false {
		t.Errorf("expected valid=false: %v", r)
	}
}

func TestNodePairVerify_validToken(t *testing.T) {
	deps := newNodeDeps()
	m := Methods(deps)

	// Create and approve a pairing to get a real token.
	pairResp := call(m, "node.pair.request", map[string]any{"nodeId": "n3"})
	pairResult := result(t, pairResp)
	requestID := pairResult["requestId"].(string)

	approveResp := call(m, "node.pair.approve", map[string]any{"requestId": requestID})
	mustOK(t, approveResp)
	approveResult := result(t, approveResp)
	nodeData := approveResult["node"].(map[string]any)
	token := nodeData["token"].(string)

	// Verify with the real token.
	resp := call(m, "node.pair.verify", map[string]any{
		"nodeId": "n3",
		"token":  token,
	})
	mustOK(t, resp)
	r := result(t, resp)
	if r["valid"].(bool) != true {
		t.Errorf("expected valid=true: %v", r)
	}
}

// ─── node.list ───────────────────────────────────────────────────────────────

func TestNodeList_empty(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.list", nil)
	mustOK(t, resp)
	r := result(t, resp)
	nodes := r["nodes"].([]any)
	if len(nodes) != 0 {
		t.Errorf("expected empty: %v", nodes)
	}
}

// ─── node.describe ───────────────────────────────────────────────────────────

func TestNodeDescribe_missingNodeID(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.describe", map[string]any{})
	mustErr(t, resp)
}

func TestNodeDescribe_notFound(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.describe", map[string]any{"nodeId": "ghost"})
	mustErr(t, resp)
}

// ─── node.rename ─────────────────────────────────────────────────────────────

func TestNodeRename_missingParams(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.rename", map[string]any{"nodeId": "n1"})
	mustErr(t, resp)
}

func TestNodeRename_notFound(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.rename", map[string]any{
		"nodeId":      "ghost",
		"displayName": "New Name",
	})
	mustErr(t, resp)
}

// ─── node.pending.* ──────────────────────────────────────────────────────────

func TestNodePendingPull_emptyQueue(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pending.pull", map[string]any{"nodeId": "n1"})
	mustOK(t, resp)
	r := result(t, resp)
	actions := r["actions"].([]any)
	if len(actions) != 0 {
		t.Errorf("expected empty actions: %v", actions)
	}
}

func TestNodePendingAck_missingIDs(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pending.ack", map[string]any{"nodeId": "n1"})
	mustErr(t, resp)
}

func TestNodePendingDrain_emptyQueue(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pending.drain", map[string]any{"nodeId": "n1"})
	mustOK(t, resp)
	r := result(t, resp)
	if r["hasMore"].(bool) != false {
		t.Errorf("expected hasMore=false: %v", r)
	}
}

func TestNodePendingEnqueue_missingParams(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pending.enqueue", map[string]any{"nodeId": "n1"})
	mustErr(t, resp)
}

func TestNodePendingEnqueue_success(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.pending.enqueue", map[string]any{
		"nodeId": "n1",
		"type":   "wake",
	})
	mustOK(t, resp)
}

// ─── node.event ──────────────────────────────────────────────────────────────

func TestNodeEvent_missingEvent(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.event", map[string]any{"payload": "x"})
	mustErr(t, resp)
}

func TestNodeEvent_broadcasts(t *testing.T) {
	var capturedEvent string
	deps := Deps{
		Nodes: nodemod.NewManager(),
		Broadcaster: func(event string, _ any) (int, []error) {
			capturedEvent = event
			return 1, nil
		},
	}
	m := Methods(deps)
	resp := call(m, "node.event", map[string]any{"event": "heartbeat"})
	mustOK(t, resp)
	if capturedEvent != "node.event.heartbeat" {
		t.Errorf("expected node.event.heartbeat, got %q", capturedEvent)
	}
}

func TestNodeEvent_nilBroadcaster(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.event", map[string]any{"event": "ping"})
	mustOK(t, resp)
}

// ─── node.invoke.result ──────────────────────────────────────────────────────

func TestNodeInvokeResult_missingIdempotencyKey(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.invoke.result", map[string]any{"ok": true})
	mustErr(t, resp)
}

func TestNodeInvokeResult_unregisteredKey(t *testing.T) {
	m := Methods(newNodeDeps())
	resp := call(m, "node.invoke.result", map[string]any{
		"idempotencyKey": "no-waiter",
		"ok":             true,
	})
	mustOK(t, resp)
	r := result(t, resp)
	if r["resolved"].(bool) != false {
		t.Errorf("expected resolved=false: %v", r)
	}
}

// ─── marshalJSON helper ──────────────────────────────────────────────────────

func TestMarshalJSON_nil(t *testing.T) {
	if got := marshalJSON(nil); got != "" {
		t.Errorf("expected empty string for nil: %q", got)
	}
}

func TestMarshalJSON_value(t *testing.T) {
	got := marshalJSON(map[string]string{"key": "val"})
	if got == "" {
		t.Error("expected non-empty JSON")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
	if m["key"] != "val" {
		t.Errorf("unexpected: %v", m)
	}
}

// ─── device.pair.list ────────────────────────────────────────────────────────

func TestDevicePairList_empty(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.list", nil)
	mustOK(t, resp)
	r := result(t, resp)
	pairs := r["pairs"].([]any)
	if len(pairs) != 0 {
		t.Errorf("expected empty: %v", pairs)
	}
}

// ─── device.pair.approve ─────────────────────────────────────────────────────

func TestDevicePairApprove_missingRequestID(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.approve", map[string]any{})
	mustErr(t, resp)
}

func TestDevicePairApprove_notFound(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.approve", map[string]any{"requestId": "ghost"})
	mustErr(t, resp)
}

// ─── device.pair.reject ──────────────────────────────────────────────────────

func TestDevicePairReject_missingRequestID(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.reject", map[string]any{})
	mustErr(t, resp)
}

func TestDevicePairReject_notFound(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.reject", map[string]any{"requestId": "ghost"})
	mustErr(t, resp)
}

// ─── device.pair.remove ──────────────────────────────────────────────────────

func TestDevicePairRemove_missingDeviceID(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.remove", map[string]any{})
	mustErr(t, resp)
}

func TestDevicePairRemove_notFound(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.pair.remove", map[string]any{"deviceId": "ghost"})
	mustErr(t, resp)
}

// ─── device.token.rotate / revoke ────────────────────────────────────────────

func TestDeviceTokenRotate_missingDeviceID(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.token.rotate", map[string]any{})
	mustErr(t, resp)
}

func TestDeviceTokenRotate_notFound(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.token.rotate", map[string]any{"deviceId": "ghost"})
	mustErr(t, resp)
}

func TestDeviceTokenRevoke_missingDeviceID(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.token.revoke", map[string]any{})
	mustErr(t, resp)
}

func TestDeviceTokenRevoke_notFound(t *testing.T) {
	m := DeviceMethods(newDeviceDeps())
	resp := call(m, "device.token.revoke", map[string]any{"deviceId": "ghost"})
	mustErr(t, resp)
}
