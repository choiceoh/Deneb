package rpc

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func newTestGatewaySubs() *events.GatewayEventSubscriptions {
	b := events.NewBroadcaster()
	b.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return events.NewGatewayEventSubscriptions(events.GatewaySubscriptionParams{
		Broadcaster: b,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
}

func testExtendedDeps() ExtendedDeps {
	gs := newTestGatewaySubs()
	return ExtendedDeps{
		Sessions:    session.NewManager(),
		GatewaySubs: gs,
	}
}

func testAgentDispatcher(deps ExtendedDeps) *Dispatcher {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{
		Sessions:    deps.Sessions,
		GatewaySubs: deps.GatewaySubs,
	})
	RegisterExtendedMethods(d, deps)
	return d
}

func TestSessionsCreate(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	resp := dispatch(t, d, "sessions.create", map[string]string{
		"key":  "test-session",
		"kind": "direct",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	s := deps.Sessions.Get("test-session")
	if s == nil {
		t.Fatal("session should exist")
	}
	if s.Kind != session.KindDirect {
		t.Errorf("Kind = %q, want %q", s.Kind, session.KindDirect)
	}
}

func TestSessionsCreate_MissingKey(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	resp := dispatch(t, d, "sessions.create", map[string]string{"kind": "direct"})
	if resp.OK {
		t.Error("expected error for missing key")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestSessionsLifecycle_FullFields(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	// Create session first.
	dispatch(t, d, "sessions.create", map[string]string{
		"key":  "lc-test",
		"kind": "direct",
	})

	// Apply start event.
	resp := dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":   "lc-test",
		"phase": "start",
		"ts":    1000,
	})
	if !resp.OK {
		t.Fatalf("start: expected ok, got error: %+v", resp.Error)
	}

	s := deps.Sessions.Get("lc-test")
	if s == nil {
		t.Fatal("session should exist")
	}
	if s.Status != session.StatusRunning {
		t.Errorf("Status = %q, want %q", s.Status, session.StatusRunning)
	}
	if s.AbortedLastRun {
		t.Error("AbortedLastRun should be false after start")
	}

	// Apply end event with stopReason=aborted (killed).
	resp = dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":        "lc-test",
		"phase":      "end",
		"ts":         2000,
		"stopReason": "aborted",
	})
	if !resp.OK {
		t.Fatalf("end: expected ok, got error: %+v", resp.Error)
	}

	s = deps.Sessions.Get("lc-test")
	if s.Status != session.StatusKilled {
		t.Errorf("Status = %q, want %q", s.Status, session.StatusKilled)
	}
	if !s.AbortedLastRun {
		t.Error("AbortedLastRun should be true after killed")
	}
}

func TestSessionsLifecycle_WithStartedAtEndedAt(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	// Start with explicit startedAt.
	sa := int64(500)
	resp := dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":       "ts-test",
		"phase":     "start",
		"ts":        1000,
		"startedAt": sa,
	})
	if !resp.OK {
		t.Fatalf("start: expected ok, got error: %+v", resp.Error)
	}

	s := deps.Sessions.Get("ts-test")
	if s.StartedAt == nil || *s.StartedAt != 500 {
		t.Errorf("StartedAt = %v, want 500", s.StartedAt)
	}

	// End with explicit endedAt.
	ea := int64(1800)
	resp = dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":     "ts-test",
		"phase":   "end",
		"ts":      2000,
		"endedAt": ea,
	})
	if !resp.OK {
		t.Fatalf("end: expected ok, got error: %+v", resp.Error)
	}

	s = deps.Sessions.Get("ts-test")
	if s.EndedAt == nil || *s.EndedAt != 1800 {
		t.Errorf("EndedAt = %v, want 1800", s.EndedAt)
	}
	if s.RuntimeMs == nil || *s.RuntimeMs != 1300 {
		t.Errorf("RuntimeMs = %v, want 1300 (1800-500)", s.RuntimeMs)
	}
}

func TestSessionsLifecycle_TimeoutAborted(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key": "to-test", "phase": "start", "ts": 1000,
	})

	resp := dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":     "to-test",
		"phase":   "end",
		"ts":      5000,
		"aborted": true,
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	s := deps.Sessions.Get("to-test")
	if s.Status != session.StatusTimeout {
		t.Errorf("Status = %q, want %q", s.Status, session.StatusTimeout)
	}
}

func TestSessionsLifecycle_Error(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key": "err-test", "phase": "start", "ts": 1000,
	})

	resp := dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key": "err-test", "phase": "error", "ts": 1500,
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	s := deps.Sessions.Get("err-test")
	if s.Status != session.StatusFailed {
		t.Errorf("Status = %q, want %q", s.Status, session.StatusFailed)
	}
}

func TestSessionsLifecycle_ResponseIncludesAbortedLastRun(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key": "resp-test", "phase": "start", "ts": 1000,
	})

	resp := dispatch(t, d, "sessions.lifecycle", map[string]any{
		"key":        "resp-test",
		"phase":      "end",
		"ts":         2000,
		"stopReason": "aborted",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	// Verify the response payload includes abortedLastRun.
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload["abortedLastRun"] != true {
		t.Errorf("response payload abortedLastRun = %v, want true", payload["abortedLastRun"])
	}
}

func TestSessionsDelete_EmitsLifecycleEvent(t *testing.T) {
	deps := testExtendedDeps()
	defer deps.GatewaySubs.Stop()
	d := testAgentDispatcher(deps)

	// Create a session, then delete it.
	deps.Sessions.Create("del-test", session.KindDirect)

	resp := dispatch(t, d, "sessions.delete", map[string]string{"key": "del-test"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	// Give the async lifecycle channel time to process.
	time.Sleep(10 * time.Millisecond)

	if deps.Sessions.Get("del-test") != nil {
		t.Error("session should have been deleted")
	}
}
