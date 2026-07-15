package toolwire

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// mockRegistrar collects registered tools for assertion.
type mockRegistrar struct {
	tools []toolport.ToolDef
}

func (m *mockRegistrar) RegisterTool(def toolport.ToolDef) {
	m.tools = append(m.tools, def)
}

func (m *mockRegistrar) toolNames() []string {
	names := make([]string, len(m.tools))
	for i, t := range m.tools {
		names[i] = t.Name
	}
	return names
}

// ─── RegisterFileTools ──────────────────────────────────────────────────────────

func TestRegisterFileToolsCreatesOnlyFileToolSet(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterFileTools(reg, t.TempDir())

	want := []string{"read", "write", "edit", "grep"}
	got := reg.toolNames()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}

	for _, name := range want {
		assertRegisteredContract(t, registeredTool(t, reg, name), name == "edit")
	}
}

// ─── registerProcessTools ─────────────────────────────────────────────────────

func TestRegisterProcessToolsCreatesExecAndProcessContracts(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &tooldeps.ProcessDeps{WorkspaceDir: t.TempDir()}
	registerProcessTools(reg, deps)

	assertRegisteredContract(t, registeredTool(t, reg, "exec"), false)
	assertRegisteredContract(t, registeredTool(t, reg, "process"), true)
}

func TestRegisterSessionToolsContracts(t *testing.T) {
	reg := &mockRegistrar{}
	registerSessionTools(reg, &tooldeps.SessionDeps{})

	assertRegisteredContract(t, registeredTool(t, reg, "sessions"), true)
	assertRegisteredContract(t, registeredTool(t, reg, "sessions_spawn"), false)
	assertRegisteredContract(t, registeredTool(t, reg, "subagents"), true)
}

func TestRegisterHeartbeatContract(t *testing.T) {
	reg := &mockRegistrar{}
	registerChronoTools(reg)

	assertRegisteredContract(t, registeredTool(t, reg, "heartbeat_update"), false)
}

func TestRegisterMediaToolsCreatesDeferredContracts(t *testing.T) {
	reg := &mockRegistrar{}
	registerMediaTools(reg, t.TempDir())

	for _, name := range []string{"send_file", "chart", "diagram", "watch"} {
		def := registeredTool(t, reg, name)
		assertRegisteredContract(t, def, true)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func registeredTool(t *testing.T, registrar *mockRegistrar, name string) toolport.ToolDef {
	t.Helper()
	for _, def := range registrar.tools {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("tool %q was not registered", name)
	return toolport.ToolDef{}
}

func assertRegisteredContract(t *testing.T, def toolport.ToolDef, deferred bool) {
	t.Helper()
	if def.Fn == nil {
		t.Fatalf("tool %q has no implementation", def.Name)
	}
	if def.InputSchema["type"] != "object" {
		t.Fatalf("tool %q schema type = %v, want object", def.Name, def.InputSchema["type"])
	}
	if def.Deferred != deferred {
		t.Fatalf("tool %q deferred = %v, want %v", def.Name, def.Deferred, deferred)
	}
}
