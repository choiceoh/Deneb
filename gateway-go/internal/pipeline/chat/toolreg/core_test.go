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

	// batch_read was removed — RegisterAdvancedTools is now a no-op.
	if len(reg.tools) != 0 {
		t.Fatalf("expected RegisterAdvancedTools to register no tools (batch_read removed), got %d", len(reg.tools))
	}
}

// ─── RegisterHiddenTools ──────────────────────────────────────────────────────


// ─── helpers ──────────────────────────────────────────────────────────────────

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
