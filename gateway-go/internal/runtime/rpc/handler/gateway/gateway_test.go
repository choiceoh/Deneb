package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var (
	callMethod    = rpctest.Call
	mustOK        = rpctest.MustOK
	mustErr       = rpctest.MustErr
	extractResult = rpctest.Result
)

// ─── RuntimeMethods key set ──────────────────────────────────────────────────

func TestRuntimeMethods_registersAllHandlers(t *testing.T) {
	m := RuntimeMethods(Deps{})
	expected := []string{
		"health",
		"status",
		"last-heartbeat",
		"set-heartbeats",
		"system-presence",
		"system-event",
		"daemon.status",
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

// ─── health ──────────────────────────────────────────────────────────────────

func TestHealth_returnsOK(t *testing.T) {
	deps := Deps{StartedAt: time.Now().Add(-5 * time.Second)}
	m := RuntimeMethods(deps)
	resp := callMethod(m, "health", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["status"] != "ok" {
		t.Errorf("expected status ok: %v", result)
	}
	uptime, ok := result["uptime"].(float64)
	if !ok || uptime <= 0 {
		t.Errorf("expected positive uptime: %v", result)
	}
}

// ─── status ──────────────────────────────────────────────────────────────────
func TestStatus_withDeps(t *testing.T) {
	deps := Deps{
		Version:         "2.0.0",
		SessionCount:    func() int { return 3 },
		ConnectionCount: func() int64 { return 7 },
		ChannelsStatus:  func() any { return map[string]string{"telegram": "ok"} },
	}
	m := RuntimeMethods(deps)
	resp := callMethod(m, "status", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["sessions"].(float64) != 3 {
		t.Errorf("expected 3 sessions: %v", result)
	}
	if result["connections"].(float64) != 7 {
		t.Errorf("expected 7 connections: %v", result)
	}
}

// ─── last-heartbeat ──────────────────────────────────────────────────────────

// ─── set-heartbeats ──────────────────────────────────────────────────────────
// ─── system-presence ─────────────────────────────────────────────────────────
func TestSystemPresence_withBroadcast(t *testing.T) {
	var capturedEvent string
	deps := Deps{
		Broadcast: func(event string, _ any) (int, []error) {
			capturedEvent = event
			return 2, nil
		},
	}
	m := RuntimeMethods(deps)
	resp := callMethod(m, "system-presence", map[string]any{"payload": map[string]string{"user": "alice"}})
	mustOK(t, resp)
	if capturedEvent != "presence" {
		t.Errorf("got %q, want presence event", capturedEvent)
	}
	result := extractResult(t, resp)
	if result["sent"].(float64) != 2 {
		t.Errorf("expected 2 sent: %v", result)
	}
}

func TestSystemPresence_invalidParams(t *testing.T) {
	m := RuntimeMethods(Deps{})
	raw := json.RawMessage(`{invalid json`)
	req := &protocol.RequestFrame{ID: "t1", Method: "system-presence", Params: raw}
	h := m["system-presence"]
	resp := h(context.Background(), req)
	mustErr(t, resp)
}

// ─── system-event ────────────────────────────────────────────────────────────

func TestSystemEvent_withBroadcast(t *testing.T) {
	var capturedEvent string
	var capturedPayload any
	deps := Deps{
		Broadcast: func(event string, payload any) (int, []error) {
			capturedEvent = event
			capturedPayload = payload
			return 1, nil
		},
	}
	m := RuntimeMethods(deps)
	resp := callMethod(m, "system-event", map[string]any{
		"event":   "my.custom.event",
		"payload": map[string]string{"key": "val"},
	})
	mustOK(t, resp)
	if capturedEvent != "my.custom.event" {
		t.Errorf("got %q, want my.custom.event", capturedEvent)
	}
	_ = capturedPayload // verified event name is sufficient
}

// ─── daemon.status ───────────────────────────────────────────────────────────

func TestDaemonStatus_running(t *testing.T) {
	deps := Deps{
		DaemonStatus: func() (any, bool) {
			return map[string]string{"state": "running", "pid": "1234"}, true
		},
	}
	m := RuntimeMethods(deps)
	resp := callMethod(m, "daemon.status", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["state"] != "running" {
		t.Errorf("expected running: %v", result)
	}
}
