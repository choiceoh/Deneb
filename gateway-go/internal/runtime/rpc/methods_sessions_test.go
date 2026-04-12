package rpc

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testSessionDeps() SessionDeps {
	return SessionDeps{
		Sessions: session.NewManager(),
		// GatewaySubs left nil — emitSessionLifecycle safely no-ops.
	}
}

func sessionDispatcher(t *testing.T) (*Dispatcher, SessionDeps) {
	t.Helper()
	deps := testSessionDeps()
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, deps)
	RegisterSessionMethods(d, deps)
	return d, deps
}

func dispatchJSON(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, *protocol.ResponseFrame) {
	t.Helper()
	resp := dispatch(t, d, method, params)
	var payload map[string]any
	if resp.Payload != nil {
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
	}
	return payload, resp
}

// ---------------------------------------------------------------------------
// tools.catalog
// ---------------------------------------------------------------------------

func TestToolsCatalog_ReturnsGroups(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", nil)
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["agentId"] != "default" {
		t.Errorf("got %v, want agentId=default", payload["agentId"])
	}
	groups, ok := payload["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("got %v, want non-empty groups array", payload["groups"])
	}
	// Verify first group is "fs" / "Files".
	first := groups[0].(map[string]any)
	if first["id"] != "fs" {
		t.Errorf("got %v, want first group id=fs", first["id"])
	}
	if first["label"] != "Files" {
		t.Errorf("got %v, want first group label=Files", first["label"])
	}
	profiles, ok := payload["profiles"].([]any)
	if !ok || len(profiles) != 4 {
		t.Errorf("got %v, want 4 profiles", payload["profiles"])
	}
}

func TestToolsCatalog_CoreToolCount(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", nil)
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	groups := payload["groups"].([]any)
	total := 0
	for _, g := range groups {
		total += len(g.(map[string]any)["tools"].([]any))
	}
	if total != 15 {
		t.Errorf("got %d, want 15 core tools", total)
	}
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

func TestSessionsPatch_AppliesFields(t *testing.T) {
	d, deps := sessionDispatcher(t)
	// Create a session first.
	deps.Sessions.Create("test-session", session.KindDirect)

	payload, resp := dispatchJSON(t, d, "sessions.patch", map[string]any{
		"key":   "test-session",
		"label": "My Session",
		"model": "claude-3",
	})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("got %v, want ok=true", payload["ok"])
	}
	if payload["key"] != "test-session" {
		t.Errorf("got %v, want key=test-session", payload["key"])
	}

	// Verify in-memory state.
	s := deps.Sessions.Get("test-session")
	if s.Label != "My Session" {
		t.Errorf("got %v, want label=My Session", s.Label)
	}
	if s.Model != "claude-3" {
		t.Errorf("got %v, want model=claude-3", s.Model)
	}
}

func TestSessionsPatch_CreatesIfNotExists(t *testing.T) {
	d, deps := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.patch", map[string]any{
		"key":   "new-session",
		"label": "Auto-created",
	})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	s := deps.Sessions.Get("new-session")
	if s == nil {
		t.Fatal("expected session to be created")
	}
	if s.Label != "Auto-created" {
		t.Errorf("got %v, want label=Auto-created", s.Label)
	}
}

// ---------------------------------------------------------------------------
// sessions.reset
// ---------------------------------------------------------------------------

func TestSessionsReset_ClearsState(t *testing.T) {
	d, deps := sessionDispatcher(t)
	sm := deps.Sessions
	sm.Create("reset-me", session.KindDirect)
	sm.ApplyLifecycleEvent("reset-me", session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1000})

	// Verify running.
	s := sm.Get("reset-me")
	if s.Status != session.StatusRunning {
		t.Fatalf("got %v, want running", s.Status)
	}

	_, resp := dispatchJSON(t, d, "sessions.reset", map[string]any{"key": "reset-me"})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}

	s = sm.Get("reset-me")
	if s.Status != "" {
		t.Errorf("got %v, want empty status after reset", s.Status)
	}
	if s.StartedAt != nil {
		t.Errorf("expected nil startedAt after reset")
	}
}

// ---------------------------------------------------------------------------
// RegisterSessionMethods registration test
// ---------------------------------------------------------------------------

func TestSessionMethodsRegistered(t *testing.T) {
	d, _ := sessionDispatcher(t)
	methods := d.Methods()
	set := make(map[string]struct{})
	for _, m := range methods {
		set[m] = struct{}{}
	}
	expected := []string{
		"sessions.patch", "sessions.reset",
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			t.Errorf("expected method %q to be registered", e)
		}
	}
}
