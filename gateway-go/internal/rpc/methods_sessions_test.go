package rpc

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testSessionDeps() SessionDeps {
	return SessionDeps{
		Deps: Deps{
			Sessions: session.NewManager(),
			Channels: channel.NewRegistry(),
			// GatewaySubs left nil — emitSessionLifecycle safely no-ops.
		},
		Forwarder: nil, // no bridge in unit tests
	}
}

func sessionDispatcher(t *testing.T) (*Dispatcher, SessionDeps) {
	t.Helper()
	deps := testSessionDeps()
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, deps.Deps)
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
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["agentId"] != "default" {
		t.Errorf("expected agentId=default, got %v", payload["agentId"])
	}
	groups, ok := payload["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("expected non-empty groups array, got %v", payload["groups"])
	}
	// Verify first group is "fs" / "Files".
	first := groups[0].(map[string]any)
	if first["id"] != "fs" {
		t.Errorf("expected first group id=fs, got %v", first["id"])
	}
	if first["label"] != "Files" {
		t.Errorf("expected first group label=Files, got %v", first["label"])
	}
	profiles, ok := payload["profiles"].([]any)
	if !ok || len(profiles) != 4 {
		t.Errorf("expected 4 profiles, got %v", payload["profiles"])
	}
}

func TestToolsCatalog_CustomAgentID(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", map[string]string{"agentId": "my-agent"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["agentId"] != "my-agent" {
		t.Errorf("expected agentId=my-agent, got %v", payload["agentId"])
	}
}

func TestToolsCatalog_FiltersEmptyGroups(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	groups := payload["groups"].([]any)
	for _, g := range groups {
		group := g.(map[string]any)
		tools := group["tools"].([]any)
		if len(tools) == 0 {
			t.Errorf("group %q should have been filtered out (empty tools)", group["id"])
		}
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
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true, got %v", payload["ok"])
	}
	if payload["key"] != "test-session" {
		t.Errorf("expected key=test-session, got %v", payload["key"])
	}

	// Verify in-memory state.
	s := deps.Sessions.Get("test-session")
	if s.Label != "My Session" {
		t.Errorf("expected label=My Session, got %v", s.Label)
	}
	if s.Model != "claude-3" {
		t.Errorf("expected model=claude-3, got %v", s.Model)
	}
}

func TestSessionsPatch_MissingKey(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.patch", map[string]any{
		"label": "something",
	})
	if resp.OK {
		t.Fatal("expected error for missing key")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %v", resp.Error.Code)
	}
}

func TestSessionsPatch_CreatesIfNotExists(t *testing.T) {
	d, deps := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.patch", map[string]any{
		"key":   "new-session",
		"label": "Auto-created",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	s := deps.Sessions.Get("new-session")
	if s == nil {
		t.Fatal("expected session to be created")
	}
	if s.Label != "Auto-created" {
		t.Errorf("expected label=Auto-created, got %v", s.Label)
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
		t.Fatalf("expected running, got %v", s.Status)
	}

	_, resp := dispatchJSON(t, d, "sessions.reset", map[string]any{"key": "reset-me"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}

	s = sm.Get("reset-me")
	if s.Status != "" {
		t.Errorf("expected empty status after reset, got %v", s.Status)
	}
	if s.StartedAt != nil {
		t.Errorf("expected nil startedAt after reset")
	}
}

func TestSessionsReset_NotFound(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.reset", map[string]any{"key": "nonexistent"})
	if resp.OK {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionsReset_ReasonNew(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("s1", session.KindDirect)

	payload, resp := dispatchJSON(t, d, "sessions.reset", map[string]any{
		"key":    "s1",
		"reason": "new",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true")
	}
}

// ---------------------------------------------------------------------------
// sessions.preview — without bridge returns missing
// ---------------------------------------------------------------------------

func TestSessionsPreview_EmptyKeys(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "sessions.preview", map[string]any{
		"keys": []string{},
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	previews := payload["previews"].([]any)
	if len(previews) != 0 {
		t.Errorf("expected empty previews, got %d", len(previews))
	}
}

func TestSessionsPreview_NoBridge_ReturnsMissing(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "sessions.preview", map[string]any{
		"keys": []string{"session-1", "session-2"},
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	previews := payload["previews"].([]any)
	if len(previews) != 2 {
		t.Fatalf("expected 2 previews, got %d", len(previews))
	}
	for _, p := range previews {
		preview := p.(map[string]any)
		if preview["status"] != "missing" {
			t.Errorf("expected status=missing, got %v", preview["status"])
		}
	}
}

// ---------------------------------------------------------------------------
// sessions.resolve
// ---------------------------------------------------------------------------

func TestSessionsResolve_ByKey(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("my-session", session.KindDirect)

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"key": "my-session"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true, got %v", payload["ok"])
	}
	if payload["key"] != "my-session" {
		t.Errorf("expected key=my-session, got %v", payload["key"])
	}
}

func TestSessionsResolve_BySessionID(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("sid-session", session.KindDirect)
	// SessionID isn't a patch field, so set it directly via Get+Set.
	s := deps.Sessions.Get("sid-session")
	s.SessionID = "uuid-123"
	deps.Sessions.Set(s)

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"sessionId": "uuid-123"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true")
	}
	if payload["key"] != "sid-session" {
		t.Errorf("expected key=sid-session, got %v", payload["key"])
	}
}

func TestSessionsResolve_ByLabel(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("labeled", session.KindDirect)
	label := "test-label"
	deps.Sessions.Patch("labeled", session.PatchFields{Label: &label})

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"label": "test-label"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true")
	}
	if payload["key"] != "labeled" {
		t.Errorf("expected key=labeled, got %v", payload["key"])
	}
}

func TestSessionsResolve_MissingIdentifier(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{})
	if resp.OK {
		t.Fatal("expected error for missing identifier")
	}
}

func TestSessionsResolve_MultipleIdentifiers(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{
		"key":       "a",
		"sessionId": "b",
	})
	if resp.OK {
		t.Fatal("expected error for multiple identifiers")
	}
}

func TestSessionsResolve_NotFound(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"key": "nope"})
	if resp.OK {
		t.Fatal("expected error response for not-found session")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND error, got %+v", resp.Error)
	}
}

func TestSessionsResolve_ExcludesGlobalByDefault(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("global-session", session.KindGlobal)
	label := "global-label"
	deps.Sessions.Patch("global-session", session.PatchFields{Label: &label})

	// Without includeGlobal=true, global session should not be found by label.
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"label": "global-label"})
	if resp.OK {
		t.Fatal("expected error: global session should be excluded by default")
	}

	// With includeGlobal=true, should find it.
	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{
		"label":         "global-label",
		"includeGlobal": true,
	})
	if !resp.OK {
		t.Fatalf("expected ok with includeGlobal=true, got error: %+v", resp.Error)
	}
	if payload["key"] != "global-session" {
		t.Errorf("expected key=global-session, got %v", payload["key"])
	}
}

func TestSessionsResolve_KeyBypassesKindFilter(t *testing.T) {
	d, deps := sessionDispatcher(t)
	// Global sessions should still be found by direct key lookup.
	deps.Sessions.Create("global-key", session.KindGlobal)

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"key": "global-key"})
	if !resp.OK {
		t.Fatalf("expected ok for direct key lookup of global, got error: %+v", resp.Error)
	}
	if payload["key"] != "global-key" {
		t.Errorf("expected key=global-key, got %v", payload["key"])
	}
}

// ---------------------------------------------------------------------------
// sessions.compact — without bridge returns not-compacted
// ---------------------------------------------------------------------------

func TestSessionsCompact_NoBridge(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "sessions.compact", map[string]any{"key": "some-session"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if payload["compacted"] != false {
		t.Errorf("expected compacted=false without bridge, got %v", payload["compacted"])
	}
}

func TestSessionsCompact_MissingKey(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.compact", map[string]any{})
	if resp.OK {
		t.Fatal("expected error for missing key")
	}
}

// ---------------------------------------------------------------------------
// session.PatchFields unit tests
// ---------------------------------------------------------------------------

func TestApplyPatch_PartialUpdate(t *testing.T) {
	s := &session.Session{Key: "s1", Kind: session.KindDirect, Model: "old-model", Label: "old"}
	label := "new"
	changed := s.ApplyPatch(session.PatchFields{Label: &label})
	if !changed {
		t.Error("expected changed=true")
	}
	if s.Label != "new" {
		t.Errorf("expected label=new, got %v", s.Label)
	}
	if s.Model != "old-model" {
		t.Errorf("expected model unchanged, got %v", s.Model)
	}
}

func TestApplyPatch_NoChange(t *testing.T) {
	s := &session.Session{Key: "s1", Label: "same"}
	label := "same"
	changed := s.ApplyPatch(session.PatchFields{Label: &label})
	if changed {
		t.Error("expected changed=false when value is same")
	}
}

func TestApplyPatch_FastMode(t *testing.T) {
	s := &session.Session{Key: "s1"}
	fast := true
	s.ApplyPatch(session.PatchFields{FastMode: &fast})
	if s.FastMode == nil || !*s.FastMode {
		t.Error("expected FastMode=true")
	}
	fast = false
	s.ApplyPatch(session.PatchFields{FastMode: &fast})
	if s.FastMode == nil || *s.FastMode {
		t.Error("expected FastMode=false")
	}
}

// ---------------------------------------------------------------------------
// Manager helper tests
// ---------------------------------------------------------------------------

func TestManager_ResetSession(t *testing.T) {
	m := session.NewManager()
	m.Create("s1", session.KindDirect)
	m.ApplyLifecycleEvent("s1", session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1000})

	s := m.ResetSession("s1")
	if s == nil {
		t.Fatal("expected session after reset")
	}
	if s.Status != "" {
		t.Errorf("expected empty status, got %v", s.Status)
	}
}

func TestManager_ResetSession_NotFound(t *testing.T) {
	m := session.NewManager()
	s := m.ResetSession("missing")
	if s != nil {
		t.Error("expected nil for missing session")
	}
}

func TestManager_FindBySessionID(t *testing.T) {
	m := session.NewManager()
	s := m.Create("s1", session.KindDirect)
	s.SessionID = "uuid-456"
	m.Set(s)

	found := m.FindBySessionID("uuid-456")
	if found == nil {
		t.Fatal("expected to find session by sessionId")
	}
	if found.Key != "s1" {
		t.Errorf("expected key=s1, got %v", found.Key)
	}
}

func TestManager_FindByLabel(t *testing.T) {
	m := session.NewManager()
	label := "my-label"
	m.Create("s1", session.KindDirect)
	m.Patch("s1", session.PatchFields{Label: &label})

	matches := m.FindByLabel("my-label")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Key != "s1" {
		t.Errorf("expected key=s1, got %v", matches[0].Key)
	}
}

func TestManager_ClearTokens(t *testing.T) {
	m := session.NewManager()
	s := m.Create("s1", session.KindDirect)
	tokens := int64(100)
	s.InputTokens = &tokens
	s.OutputTokens = &tokens
	s.TotalTokens = &tokens
	m.Set(s)

	m.ClearTokens("s1")
	s = m.Get("s1")
	if s.InputTokens != nil || s.OutputTokens != nil || s.TotalTokens != nil {
		t.Error("expected nil token fields after ClearTokens")
	}
}

// ---------------------------------------------------------------------------
// RegisterSessionMethods registration test
// ---------------------------------------------------------------------------

func TestSessionMethodsRegistered(t *testing.T) {
	d, _ := sessionDispatcher(t)
	methods := d.Methods()
	set := make(map[string]bool)
	for _, m := range methods {
		set[m] = true
	}
	expected := []string{
		"sessions.patch", "sessions.reset", "sessions.preview",
		"sessions.resolve", "sessions.compact",
	}
	for _, e := range expected {
		if !set[e] {
			t.Errorf("expected method %q to be registered", e)
		}
	}
}
