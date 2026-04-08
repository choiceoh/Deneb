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

func TestToolsCatalog_CustomAgentID(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", map[string]string{"agentId": "my-agent"})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["agentId"] != "my-agent" {
		t.Errorf("got %v, want agentId=my-agent", payload["agentId"])
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
	if total != 18 {
		t.Errorf("got %d, want 18 core tools", total)
	}
}

func TestToolsCatalog_FiltersEmptyGroups(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "tools.catalog", nil)
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
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

func TestSessionsPatch_MissingKey(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.patch", map[string]any{
		"label": "something",
	})
	if resp.OK {
		t.Fatal("expected error for missing key")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("got %v, want MISSING_PARAM", resp.Error.Code)
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
		t.Fatalf("got error: %+v, want ok", resp.Error)
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
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	previews := payload["previews"].([]any)
	if len(previews) != 0 {
		t.Errorf("got %d, want empty previews", len(previews))
	}
}

func TestSessionsPreview_NoBridge_ReturnsMissing(t *testing.T) {
	d, _ := sessionDispatcher(t)
	payload, resp := dispatchJSON(t, d, "sessions.preview", map[string]any{
		"keys": []string{"session-1", "session-2"},
	})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	previews := payload["previews"].([]any)
	if len(previews) != 2 {
		t.Fatalf("got %d, want 2 previews", len(previews))
	}
	for _, p := range previews {
		preview := p.(map[string]any)
		if preview["status"] != "missing" {
			t.Errorf("got %v, want status=missing", preview["status"])
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
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("got %v, want ok=true", payload["ok"])
	}
	if payload["key"] != "my-session" {
		t.Errorf("got %v, want key=my-session", payload["key"])
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
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true")
	}
	if payload["key"] != "sid-session" {
		t.Errorf("got %v, want key=sid-session", payload["key"])
	}
}

func TestSessionsResolve_ByLabel(t *testing.T) {
	d, deps := sessionDispatcher(t)
	deps.Sessions.Create("labeled", session.KindDirect)
	label := "test-label"
	deps.Sessions.Patch("labeled", session.PatchFields{Label: &label})

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"label": "test-label"})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true")
	}
	if payload["key"] != "labeled" {
		t.Errorf("got %v, want key=labeled", payload["key"])
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

func TestSessionsResolve_AmbiguousLabel(t *testing.T) {
	d, deps := sessionDispatcher(t)
	label := "dup-label"
	deps.Sessions.Create("s1", session.KindDirect)
	deps.Sessions.Patch("s1", session.PatchFields{Label: &label})
	deps.Sessions.Create("s2", session.KindDirect)
	deps.Sessions.Patch("s2", session.PatchFields{Label: &label})

	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"label": "dup-label"})
	if resp.OK {
		t.Fatal("expected error for ambiguous label")
	}
	if resp.Error.Code != protocol.ErrConflict {
		t.Errorf("got %v, want CONFLICT error", resp.Error.Code)
	}
}

func TestSessionsResolve_AgentIDFilter(t *testing.T) {
	d, deps := sessionDispatcher(t)
	label := "agent-label"
	// Default agent session.
	deps.Sessions.Create("my-session", session.KindDirect)
	deps.Sessions.Patch("my-session", session.PatchFields{Label: &label})
	// Non-default agent session with same label.
	deps.Sessions.Create("agent:other:my-session", session.KindDirect)
	deps.Sessions.Patch("agent:other:my-session", session.PatchFields{Label: &label})

	// Without agentId filter, ambiguous.
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"label": "agent-label"})
	if resp.OK {
		t.Fatal("expected conflict without agent filter")
	}

	// With agentId=default, only the default agent session matches.
	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{
		"label":   "agent-label",
		"agentId": "default",
	})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok with agentId filter", resp.Error)
	}
	if payload["key"] != "my-session" {
		t.Errorf("got %v, want key=my-session", payload["key"])
	}
}

func TestSessionsResolve_NotFound(t *testing.T) {
	d, _ := sessionDispatcher(t)
	_, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"key": "nope"})
	if resp.OK {
		t.Fatal("expected error response for not-found session")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("got %+v, want NOT_FOUND error", resp.Error)
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
		t.Fatalf("got error: %+v, want ok with includeGlobal=true", resp.Error)
	}
	if payload["key"] != "global-session" {
		t.Errorf("got %v, want key=global-session", payload["key"])
	}
}

func TestSessionsResolve_KeyBypassesKindFilter(t *testing.T) {
	d, deps := sessionDispatcher(t)
	// Global sessions should still be found by direct key lookup.
	deps.Sessions.Create("global-key", session.KindGlobal)

	payload, resp := dispatchJSON(t, d, "sessions.resolve", map[string]any{"key": "global-key"})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok for direct key lookup of global", resp.Error)
	}
	if payload["key"] != "global-key" {
		t.Errorf("got %v, want key=global-key", payload["key"])
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
		t.Errorf("got %v, want label=new", s.Label)
	}
	if s.Model != "old-model" {
		t.Errorf("got %v, want model unchanged", s.Model)
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
		t.Errorf("got %v, want empty status", s.Status)
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
		t.Errorf("got %v, want key=s1", found.Key)
	}
}

func TestManager_FindByLabel(t *testing.T) {
	m := session.NewManager()
	label := "my-label"
	m.Create("s1", session.KindDirect)
	m.Patch("s1", session.PatchFields{Label: &label})

	matches := m.FindByLabel("my-label")
	if len(matches) != 1 {
		t.Fatalf("got %d, want 1 match", len(matches))
	}
	if matches[0].Key != "s1" {
		t.Errorf("got %v, want key=s1", matches[0].Key)
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
	set := make(map[string]struct{})
	for _, m := range methods {
		set[m] = struct{}{}
	}
	expected := []string{
		"sessions.patch", "sessions.reset", "sessions.preview",
		"sessions.resolve",
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			t.Errorf("expected method %q to be registered", e)
		}
	}
}
