package toolreg

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// mockRegistrar collects registered tools for assertion.
type mockRegistrar struct {
	tools []toolctx.ToolDef
}

func (m *mockRegistrar) RegisterTool(def toolctx.ToolDef) {
	m.tools = append(m.tools, def)
}

func (m *mockRegistrar) toolNames() []string {
	names := make([]string, len(m.tools))
	for i, t := range m.tools {
		names[i] = t.Name
	}
	return names
}

// ─── buildLocalAIProbe ────────────────────────────────────────────────────────

func TestBuildLocalAIProbe_nilDeps(t *testing.T) {
	probe := buildLocalAIProbe(nil)

	if probe.CheckHealth == nil {
		t.Fatal("expected non-nil CheckHealth when deps are nil")
	}
	if probe.BaseURL == nil {
		t.Fatal("expected non-nil BaseURL when deps are nil")
	}
	// Default CheckHealth should return false (no local AI available).
	if probe.CheckHealth() {
		t.Error("expected CheckHealth to return false for nil deps")
	}
	// Default BaseURL should return the fallback URL.
	if got := probe.BaseURL(); got != "http://localhost:30000/v1" {
		t.Errorf("expected default BaseURL, got %q", got)
	}
}

func TestBuildLocalAIProbe_nonNilDeps(t *testing.T) {
	deps := &LocalAIDeps{
		CheckLocalAIHealth: func() bool { return true },
		BaseURL:            func() string { return "http://custom:9999/v1" },
	}
	probe := buildLocalAIProbe(deps)

	if !probe.CheckHealth() {
		t.Error("expected CheckHealth to return true from provided func")
	}
	if got := probe.BaseURL(); got != "http://custom:9999/v1" {
		t.Errorf("expected custom BaseURL, got %q", got)
	}
}

func TestBuildLocalAIProbe_partialDeps(t *testing.T) {
	// Only CheckLocalAIHealth set; BaseURL should get the default.
	deps := &LocalAIDeps{
		CheckLocalAIHealth: func() bool { return true },
	}
	probe := buildLocalAIProbe(deps)

	if !probe.CheckHealth() {
		t.Error("expected CheckHealth to use provided func")
	}
	if got := probe.BaseURL(); got != "http://localhost:30000/v1" {
		t.Errorf("expected default BaseURL for nil BaseURL func, got %q", got)
	}
}

// ─── FetchToolsSchema ─────────────────────────────────────────────────────────

func TestFetchToolsSchema_validStructure(t *testing.T) {
	schema := FetchToolsSchema()

	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	typ, ok := schema["type"].(string)
	if !ok || typ != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	// Should have "names" and "query" properties at minimum.
	if _, ok := props["names"]; !ok {
		t.Error("missing 'names' property in fetch_tools schema")
	}
	if _, ok := props["query"]; !ok {
		t.Error("missing 'query' property in fetch_tools schema")
	}
}

// ─── RegisterFSTools ──────────────────────────────────────────────────────────

func TestRegisterFSTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &toolctx.CoreToolDeps{WorkspaceDir: t.TempDir()}
	RegisterFSTools(reg, deps, nil)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterFSTools to register at least one tool")
	}
	// Verify well-known FS tools are present.
	names := reg.toolNames()
	for _, want := range []string{"read", "write", "edit", "grep", "find"} {
		if !containsName(names, want) {
			t.Errorf("missing expected tool %q", want)
		}
	}
}

// ─── RegisterProcessTools ─────────────────────────────────────────────────────

func TestRegisterProcessTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &toolctx.ProcessDeps{WorkspaceDir: t.TempDir()}
	RegisterProcessTools(reg, deps)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterProcessTools to register at least one tool")
	}
	names := reg.toolNames()
	for _, want := range []string{"exec", "process"} {
		if !containsName(names, want) {
			t.Errorf("missing expected tool %q", want)
		}
	}
}

// ─── RegisterDataTools ────────────────────────────────────────────────────────

func TestRegisterDataTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterDataTools(reg)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterDataTools to register at least one tool")
	}
	names := reg.toolNames()
	if !containsName(names, "kv") {
		t.Error("missing expected tool 'kv'")
	}
}

// ─── RegisterAdvancedTools ────────────────────────────────────────────────────

func TestRegisterAdvancedTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterAdvancedTools(reg, t.TempDir())

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterAdvancedTools to register at least one tool")
	}
	names := reg.toolNames()
	for _, want := range []string{"batch_read", "search_and_read", "inspect", "apply_patch"} {
		if !containsName(names, want) {
			t.Errorf("missing expected tool %q", want)
		}
	}
}

// ─── RegisterHiddenTools ──────────────────────────────────────────────────────

func TestRegisterHiddenTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterHiddenTools(reg, nil)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterHiddenTools to register at least one tool")
	}
	names := reg.toolNames()
	for _, want := range []string{"agent_logs", "gateway_logs"} {
		if !containsName(names, want) {
			t.Errorf("missing expected tool %q", want)
		}
	}
	// All hidden tools should have Hidden=true.
	for _, td := range reg.tools {
		if !td.Hidden {
			t.Errorf("tool %q should be hidden", td.Name)
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
