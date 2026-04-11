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

// ─── RegisterFSTools ──────────────────────────────────────────────────────────

func TestRegisterFSTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &toolctx.CoreToolDeps{WorkspaceDir: t.TempDir()}
	RegisterFSTools(reg, deps)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterFSTools to register at least one tool")
	}
	// Verify well-known FS tools are present.
	names := reg.toolNames()
	for _, want := range []string{"read", "write", "edit", "grep"} {
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

// ─── helpers ──────────────────────────────────────────────────────────────────

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
